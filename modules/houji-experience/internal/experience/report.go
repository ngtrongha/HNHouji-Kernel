package experience

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func fieldsKV(text string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}
func number(s string) float64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func (a *App) Report(ctx context.Context) map[string]any {
	mem := fieldsKV(a.readSystem("/proc/meminfo"))
	metrics := map[string]any{}
	if load := a.readSystem("/proc/loadavg"); load != "" {
		metrics["cpu_load_average_1m"] = number(load)
	}
	if online := a.readSystem("/sys/devices/system/cpu/online"); online != "" {
		metrics["cpu_online"] = online
	}
	for _, key := range []string{"MemTotal", "MemAvailable", "SwapTotal", "SwapFree"} {
		if v, ok := mem[key]; ok {
			metrics[key+"_kib"] = number(v)
		}
	}
	if psi := a.readSystem("/proc/pressure/memory"); psi != "" {
		for _, line := range strings.Split(psi, "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && (f[0] == "some" || f[0] == "full") {
				for _, x := range f[1:] {
					if k, v, ok := strings.Cut(x, "="); ok && k == "avg10" {
						metrics["psi_"+f[0]+"_avg10"] = number(v)
					}
				}
			}
		}
	}
	if stat := strings.Fields(a.readSystem("/sys/block/zram0/mm_stat")); len(stat) >= 3 {
		metrics["zram_original_bytes"] = number(stat[0])
		metrics["zram_compressed_bytes"] = number(stat[1])
		metrics["zram_allocated_bytes"] = number(stat[2])
	}
	if size := a.readSystem("/sys/block/zram0/disksize"); size != "" {
		metrics["zram_capacity_bytes"] = number(size)
	}
	if alg := a.readSystem("/sys/block/zram0/comp_algorithm"); alg != "" {
		metrics["zram_algorithms"] = alg
	}
	battery := fieldsKV(func() string { o, _ := a.run(ctx, "dumpsys", "battery"); return o }())
	if v, ok := battery["level"]; ok {
		metrics["battery_percent"] = number(v)
	}
	if v, ok := battery["temperature"]; ok {
		metrics["battery_celsius"] = number(v) / 10
	}
	thermal, _ := a.run(ctx, "dumpsys", "thermalservice")
	for _, line := range strings.Split(thermal, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Thermal Status:") {
			metrics["thermal_status"] = number(strings.TrimPrefix(line, "Thermal Status:"))
		}
	}
	advice := []string{}
	if v, ok := metrics["psi_full_avg10"].(float64); ok && v >= 1 {
		advice = append(advice, "Áp lực bộ nhớ FULL cao trong cửa sổ 10 giây. Đo lặp lại và giảm tác vụ nặng; không tự đổi ZRAM/LMKD.")
	}
	if v, ok := metrics["MemAvailable_kib"].(float64); ok && v < 768*1024 {
		advice = append(advice, "RAM khả dụng dưới 768 MiB. Kiểm tra ứng dụng đang dùng; không xóa cache cưỡng bức.")
	}
	if v, ok := metrics["thermal_status"].(float64); ok && v >= 3 {
		advice = append(advice, "Thermal từ SEVERE trở lên. Giảm tải, giữ cơ chế bảo vệ nhiệt của hãng.")
	}
	if len(advice) == 0 {
		advice = append(advice, "Chưa có dấu hiệu vượt ngưỡng trong mẫu hiện tại. Một mẫu chưa đủ kết luận hiệu năng hoặc pin chờ.")
	}
	advice = append(advice, "Pin chờ và cải thiện hiệu năng cần phép đo lặp; báo cáo này không ước lượng mức tiết kiệm pin.")
	return map[string]any{"source": "local-rules", "metrics": metrics, "advice": advice, "ntsync": exists(a.system("/dev/ntsync")), "legacy_conflict": exists(a.Paths.Legacy)}
}

func (a *App) Status(ctx context.Context) map[string]any {
	sync := a.KernelSync(ctx)
	c, configErr := a.config()
	if configErr != nil {
		c = disabledConfig()
	}
	r := Runtime{Profile: "observe", Reason: "not-started"}
	_ = readJSON(a.state("runtime.json"), &r)
	pidText, _ := os.ReadFile(a.state("watcher.lock"))
	fields := strings.Fields(string(pidText))
	pid := 0
	if len(fields) > 1 && fields[1] == a.bootID() {
		pid, _ = strconv.Atoi(fields[0])
	}
	j, e := a.journal()
	conflicts := 0
	for _, x := range j.Entries {
		if x.Conflict {
			conflicts++
		}
	}
	if !processAlivePositive(pid) {
		r.Profile = "observe"
		r.Reason = "watcher-offline"
	}
	return map[string]any{"version": Version, "kernel_sync": sync, "kernel_tuning": a.kernelTuningStatus(ctx, c, sync, j), "runtime": r, "config": a.snapshotConfig(), "watcher_running": processAlivePositive(pid), "journal_entries": len(j.Entries), "restore_conflicts": conflicts, "journal_valid": e == nil, "legacy_conflict": exists(a.Paths.Legacy), "module_disabled": a.moduleDisabled(), "report": a.Report(ctx), "ai": a.AIStatus()}
}
func processAlivePositive(pid int) bool { return pid > 0 && processAlive(pid) }

func (a *App) Doctor(ctx context.Context) map[string]any {
	checks := map[string]any{}
	if e := a.compatible(ctx); e != nil {
		checks["compatibility"] = e.Error()
	} else {
		checks["compatibility"] = "device-family-supported; see kernel_sync for build verification"
	}
	for _, p := range []string{"ro.build.version.sdk", "ro.build.version.release"} {
		v, e := a.run(ctx, "getprop", p)
		if e == nil {
			checks[p] = v
		}
	}
	k, _ := a.run(ctx, "uname", "-r")
	checks["kernel"] = k
	helper, _ := a.run(ctx, "cmd", "game", "help")
	checks["game_manager"] = strings.Contains(helper, "list-modes") && strings.Contains(helper, " mode ")
	checks["ntsync_device"] = exists(a.system("/dev/ntsync"))
	checks["ntsync_runner_usage"] = "requires runner log verification"
	checks["arr"] = "requires Android/Display HAL validation; not changed by this module"
	checks["stock_config"] = "IKCONFIG, HZ, ZRAM sizing/compressor and kernel pins are read-only; allowlisted runtime tuning is journaled separately"
	checks["owner_user"] = a.ownerUser(ctx)
	j, _ := a.journal()
	return map[string]any{"checks": checks, "status": a.Status(ctx), "pending_restore": j.Entries}
}

func (a *App) Export(ctx context.Context) (string, error) {
	// Export deliberately excludes package lists, secrets, questions and raw Android logs.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !a.ownerUser(ctx) {
		return "", errors.New("export requires owner user 0; no file was saved")
	}
	j, e := a.journal()
	if e != nil {
		return "", e
	}
	c, configErr := a.config()
	if configErr != nil {
		c = disabledConfig()
	}
	sync := a.KernelSync(ctx)
	data := map[string]any{"version": Version, "report": a.Report(ctx), "kernel_sync": sync, "kernel_tuning": a.kernelTuningStatus(ctx, c, sync, j), "pending_restore_count": len(j.Entries)}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	dir := a.system("/storage/emulated/0/Download/HNHouji")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", errors.New("cannot access Downloads/HNHouji; unlock shared storage and try again")
	}
	// A public download is intentionally separate from atomicWrite's private
	// directory/0600 policy. Android's emulated storage owns access/SELinux labels;
	// don't chmod/chown its files or relax any /data/adb permissions.
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Downloads/HNHouji must be a normal directory")
	}
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		return "", err
	}
	name := fmt.Sprintf("HNHouji-report-%s-%x.json", time.Now().UTC().Format("20060102-150405"), suffix)
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0664)
	if err != nil {
		return "", errors.New("cannot create report in Downloads/HNHouji")
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", errors.New("report write failed; no completed download was saved")
	}
	complete = true
	return path, nil
}

func pretty(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
