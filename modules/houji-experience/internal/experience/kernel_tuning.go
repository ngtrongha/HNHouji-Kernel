package experience

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type KernelKnob struct {
	ID      string   `json:"id"`
	Control string   `json:"control"`
	Enabled bool     `json:"enabled"`
	Level   string   `json:"level"`
	Levels  []string `json:"levels"`
	Label   string   `json:"label"`
	Target  string   `json:"target"`
	Current string   `json:"current,omitempty"`
	Before  string   `json:"before,omitempty"`
	State   string   `json:"state"`
	Detail  string   `json:"detail"`
}

var blockNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var tcpNameRE = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
var zramSwapRE = regexp.MustCompile(`^/dev/(?:block/)?zram[0-9]+$`)

func validKernelControl(id string) bool {
	return id == "swap_readahead" || id == "tcp_congestion" || id == "data_fsync"
}
func defaultKernelLevel(id string) string {
	switch id {
	case "swap_readahead":
		return "0"
	case "tcp_congestion":
		return "bbr"
	case "data_fsync":
		return "20"
	}
	return ""
}
func validKernelLevel(id, level string) bool {
	if !validKernelControl(id) {
		return false
	}
	if level == "stock" {
		return true
	}
	switch id {
	case "swap_readahead":
		return level == "0" || level == "1" || level == "2" || level == "3"
	case "tcp_congestion":
		return level == "cubic" || level == "bbr"
	case "data_fsync":
		return level == "20"
	}
	return false
}
func kernelLevel(c Config, id string) string {
	if level := c.KernelLevels[id]; level != "" {
		return level
	}
	return defaultKernelLevel(id)
}
func hasLevel(knob KernelKnob, level string) bool {
	for _, candidate := range knob.Levels {
		if candidate == level {
			return true
		}
	}
	return false
}
func kernelControlForKnob(id string) string {
	switch id {
	case "vm.page-cluster":
		return "swap_readahead"
	case "tcp.congestion":
		return "tcp_congestion"
	}
	if strings.HasPrefix(id, "f2fs:") {
		return "data_fsync"
	}
	return ""
}
func kernelControlEnabled(c Config, id string) bool {
	on, saved := c.KernelControls[id]
	return !saved || on
}

func (a *App) configuredKernelPlan(c Config) []KernelKnob {
	plan := a.kernelPlan()
	for i := range plan {
		plan[i].Control = kernelControlForKnob(plan[i].ID)
		if plan[i].ID == "f2fs" {
			plan[i].Control = "data_fsync"
		}
		plan[i].Enabled = kernelControlEnabled(c, plan[i].Control)
		plan[i].Level = kernelLevel(c, plan[i].Control)
		plan[i].Target = plan[i].Level
		if plan[i].Level == "stock" {
			plan[i].Target = ""
		}
		if plan[i].State == "available" && !hasLevel(plan[i], plan[i].Level) {
			plan[i].State = "unsupported"
		}
	}
	return plan
}

// This allowlist is shared by application and recovery. Neither RPC nor journal
// can introduce an arbitrary writable path, shell command or kernel parameter.
func kernelKnobPath(id string) (string, error) {
	switch id {
	case "vm.page-cluster":
		return "/proc/sys/vm/page-cluster", nil
	case "tcp.congestion":
		return "/proc/sys/net/ipv4/tcp_congestion_control", nil
	}
	if name, ok := strings.CutPrefix(id, "f2fs:"); ok && blockNameRE.MatchString(name) {
		return "/sys/fs/f2fs/" + name + "/min_fsync_blocks", nil
	}
	return "", errors.New("unknown kernel knob")
}

func validKernelValue(id, value string) bool {
	if id == "tcp.congestion" {
		return tcpNameRE.MatchString(value)
	}
	if _, err := kernelKnobPath(id); err != nil {
		return false
	}
	n, err := strconv.Atoi(value)
	if err != nil || strconv.Itoa(n) != value {
		return false
	}
	if id == "vm.page-cluster" {
		return n >= 0 && n <= 31
	}
	return n >= 0 && uint64(n) <= 4294967295
}

func (a *App) readKernelKnob(id string) (string, error) {
	path, err := kernelKnobPath(id)
	if err != nil {
		return "", err
	}
	f, err := os.Open(a.system(path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 256))
	value := strings.TrimSpace(string(b))
	if err != nil || !validKernelValue(id, value) {
		return "", errors.New("kernel knob unreadable or invalid")
	}
	return value, nil
}

func (a *App) writeKernelKnob(id, value string) error {
	path, err := kernelKnobPath(id)
	if err != nil {
		return err
	}
	if !validKernelValue(id, value) {
		return errors.New("invalid kernel knob value")
	}
	// Existing proc/sysfs nodes only. Do not create missing nodes, chmod them or
	// use atomic rename, which is inappropriate for kernel pseudo-filesystems.
	f, err := os.OpenFile(a.system(path), os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, value+"\n")
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (a *App) onlyZRAMSwap() bool {
	lines := strings.Split(a.readSystem("/proc/swaps"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "Filename") {
		return false
	}
	count := 0
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 5 || !zramSwapRE.MatchString(f[0]) || f[1] != "partition" {
			return false
		}
		size, err := strconv.ParseUint(f[2], 10, 64)
		if err != nil || size == 0 {
			return false
		}
		count++
	}
	return count > 0
}

// Match /data's major:minor to a mounted F2FS sysfs instance. Never tune all
// F2FS partitions, assume dm-0, or follow a user-provided device/path.
func (a *App) dataF2FS() string {
	dev := ""
	for _, line := range strings.Split(a.readSystem("/proc/self/mountinfo"), "\n") {
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		f, r := strings.Fields(left), strings.Fields(right)
		if len(f) >= 6 && len(r) >= 3 && f[4] == "/data" && r[0] == "f2fs" && strings.Contains(","+f[5]+",", ",rw,") {
			if dev != "" && dev != f[2] {
				return ""
			}
			dev = f[2]
		}
	}
	if dev == "" {
		return ""
	}
	dirs, _ := os.ReadDir(a.system("/sys/fs/f2fs"))
	found := ""
	for _, dir := range dirs {
		name := dir.Name()
		if !blockNameRE.MatchString(name) {
			continue
		}
		if a.readSystem("/sys/class/block/"+name+"/dev") == dev {
			if found != "" {
				return ""
			}
			found = name
		}
	}
	return found
}

func (a *App) kernelPlan() []KernelKnob {
	knobs := []KernelKnob{
		{ID: "vm.page-cluster", Label: "ZRAM: đọc từng trang swap", Target: "0", State: "unsupported", Detail: "Chỉ áp dụng khi swap đang hoạt động hoàn toàn trên ZRAM. Giảm đọc đón, có thể tăng số lần page fault."},
		{ID: "tcp.congestion", Label: "TCP congestion control", Target: "bbr", State: "unsupported", Detail: "Chỉ chọn BBR nếu kernel đã cung cấp. Tác động tới kết nối TCP mới; không bảo đảm giảm ping."},
		{ID: "f2fs", Label: "F2FS /data: ngưỡng fsync", Target: "20", State: "unsupported", Detail: "Chỉ dùng node min_fsync_blocks của /data đã xác định. Cần đo độ trễ ghi thực tế."},
	}
	knobs[0].Levels = []string{"stock"}
	knobs[1].Levels = []string{"stock"}
	knobs[2].Levels = []string{"stock"}
	eligible := []bool{a.onlyZRAMSwap(), false, false}
	if eligible[0] {
		knobs[0].Levels = append(knobs[0].Levels, "0", "1", "2", "3")
	}
	for _, v := range strings.Fields(a.readSystem("/proc/sys/net/ipv4/tcp_available_congestion_control")) {
		if v == "bbr" || v == "cubic" {
			eligible[1] = true
			knobs[1].Levels = append(knobs[1].Levels, v)
		}
	}
	if dev := a.dataF2FS(); dev != "" {
		knobs[2].ID = "f2fs:" + dev
		eligible[2] = true
		knobs[2].Levels = append(knobs[2].Levels, "20")
	}
	for i := range knobs {
		v, err := a.readKernelKnob(knobs[i].ID)
		if err == nil {
			knobs[i].Current = v
		}
		if eligible[i] && err == nil {
			knobs[i].State = "available"
		}
	}
	return knobs
}

func (a *App) tuneKernel(ctx context.Context, c Config, j *Journal) error {
	if !c.KernelTuning {
		return a.restoreEntries(ctx, j, func(e Entry) bool { return e.Kind == "kernel" })
	}
	plan := a.configuredKernelPlan(c)
	for i, entry := range j.Entries {
		if entry.Kind != "kernel" {
			continue
		}
		current, err := a.readKernelKnob(entry.Package)
		if err != nil {
			return err
		}
		if entry.Conflict || current != entry.Applied {
			if !entry.Conflict {
				j.Entries[i].Conflict = true
				if err := atomicJSON(a.state("journal.json"), j); err != nil {
					return err
				}
			}
			return errors.New("kernel setting changed externally; preserved")
		}
	}
	eligible := map[string]bool{}
	for _, knob := range plan {
		if knob.State == "available" && knob.Enabled && knob.Level != "stock" {
			eligible[knob.ID] = true
		}
	}
	if err := a.restoreEntries(ctx, j, func(e Entry) bool { return e.Kind == "kernel" && !eligible[e.Package] }); err != nil {
		return err
	}
	pressureChecked := false
	for _, knob := range plan {
		if knob.State != "available" || !knob.Enabled || knob.Level == "stock" {
			continue
		}
		// Existing entries passed the preflight above. Never fight subsequent
		// HyperOS writes, including a reset to the node's original value.
		owned := false
		for _, entry := range j.Entries {
			if entry.Kind == "kernel" && entry.Package == knob.ID {
				owned = true
			}
		}
		if !owned {
			if !pressureChecked {
				if err := a.pressureReady(ctx); err != nil {
					return err
				}
				pressureChecked = true
			}
			if err := a.apply(ctx, j, Entry{Kind: "kernel", Package: knob.ID, Applied: knob.Target}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) kernelTuningStatus(ctx context.Context, c Config, sync KernelSync, j Journal) map[string]any {
	knobs := a.configuredKernelPlan(c)
	active := c.Enabled && c.KernelTuning && sync.Ready && a.ownerUser(ctx) && !a.moduleDisabled() && !exists(a.Paths.Legacy) && (c.MigrationBoot == "" || c.MigrationBoot != a.bootID())
	for i := range knobs {
		knob := &knobs[i]
		if knob.State == "available" {
			if knob.Level == "stock" {
				knob.State = "stock"
			} else if !knob.Enabled {
				knob.State = "disabled"
			} else if !active {
				knob.State = "waiting"
			} else if knob.Current == knob.Target {
				knob.State = "already-set"
			}
		}
		for _, entry := range j.Entries {
			if entry.Kind == "kernel" && entry.Package == knob.ID {
				knob.Before = entry.Before
				knob.State = "pending-restore"
				if active && knob.Enabled && knob.Current == entry.Applied {
					knob.State = "applied"
				}
				if entry.Conflict || (knob.Current != "" && knob.Current != entry.Applied && knob.Current != entry.Before) {
					knob.State = "conflict"
				}
			}
		}
	}
	return map[string]any{"enabled": c.KernelTuning, "preset": "daily", "knobs": knobs}
}
