package experience

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxBatteryBytes = 2 * 1024 * 1024

func (a *App) batteryObservation() map[string]any {
	h, err := a.usageHistory()
	result := map[string]any{"source": "watcher-covered-screen-off-intervals", "observed_seconds": int64(0), "drop_percent": float64(0), "intervals": int64(0), "note": "Chỉ các khoảng có mẫu liên tiếp, không lấp khoảng ngủ/thiếu dữ liệu; bước phần trăm có độ lượng tử 1%."}
	if err != nil {
		return result
	}
	var seconds, count int64
	var drop float64
	for _, day := range h.Days {
		seconds += day.BatteryOffSeconds
		drop += day.BatteryOffDrop
		count += day.BatteryIntervals
	}
	result["observed_seconds"] = seconds
	result["drop_percent"] = drop
	result["intervals"] = count
	if seconds > 0 && drop >= 1 && a.readSystem("/sys/class/power_supply/battery/status") == "Discharging" {
		result["percent_per_hour"] = drop * 3600 / float64(seconds)
	}
	return result
}

func (a *App) batteryCheck() map[string]any {
	result := map[string]any{"source": "power_supply/battery", "units": map[string]string{"temp": "0.1 °C", "voltage_now": "µV", "current_now": "µA", "charge_full": "µAh", "charge_full_design": "µAh", "charge_counter": "µAh", "capacity": "%"}}
	for _, key := range []string{"status", "health", "technology"} {
		if v := a.readSystem("/sys/class/power_supply/battery/" + key); v != "" && len(v) < 80 {
			result[key] = v
		}
	}
	for _, key := range []string{"capacity", "temp", "voltage_now", "current_now", "charge_full", "charge_full_design", "charge_counter", "cycle_count"} {
		if v, err := strconv.ParseInt(a.readSystem("/sys/class/power_supply/battery/"+key), 10, 64); err == nil {
			if key == "capacity" && (v < 0 || v > 100) {
				continue
			}
			result[key] = v
		}
	}
	return result
}

type BatterySnapshot struct {
	Boot             string    `json:"boot"`
	At               time.Time `json:"at"`
	Source           string    `json:"source"`
	Anchor           string    `json:"anchor"`
	ScreenOffSeconds float64   `json:"screen_off_seconds"`
	ScreenOffDrop    float64   `json:"screen_off_drop_percent"`
	Rate             *float64  `json:"percent_per_hour,omitempty"`
	Note             string    `json:"note"`
}

func parseBatteryStats(raw string) (BatterySnapshot, error) {
	if len(raw) > maxBatteryBytes {
		return BatterySnapshot{}, errors.New("battery-response-too-large")
	}
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1
	s := BatterySnapshot{Source: "android-checkin-since-stats-start", Note: "Thống kê Android từ mốc reset/sạc của hệ thống; screen-off không đồng nghĩa deep sleep."}
	bt, dc, version := false, false, false
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return s, errors.New("battery-format-unsupported")
		}
		if len(row) < 4 {
			continue
		}
		if row[1] != "0" {
			continue
		}
		if row[3] == "vers" {
			if row[0] != "9" || len(row) < 6 {
				return s, errors.New("battery-format-unsupported")
			}
			version = true
		}
		if row[2] != "l" {
			continue
		}
		if (row[3] == "bt" || row[3] == "dc") && row[0] != "9" {
			return s, errors.New("battery-format-unsupported")
		}
		switch row[3] {
		case "bt":
			if bt || len(row) < 12 {
				return s, errors.New("battery-format-unsupported")
			}
			start, e1 := strconv.ParseInt(row[9], 10, 64)
			count, e2 := strconv.ParseInt(row[4], 10, 64)
			off, e3 := strconv.ParseInt(row[10], 10, 64)
			if e1 != nil || e2 != nil || e3 != nil || start < 0 || count < 0 || off < 0 {
				return s, errors.New("battery-format-unsupported")
			}
			s.Anchor = row[4] + ":" + row[9]
			s.ScreenOffSeconds = float64(off) / 1000
			bt = true
		case "dc":
			if dc || len(row) < 8 {
				return s, errors.New("battery-format-unsupported")
			}
			drop, e := strconv.ParseFloat(row[7], 64)
			if e != nil || drop < 0 || drop > 10000 || strings.ContainsAny(row[7], "NnIi") {
				return s, errors.New("battery-format-unsupported")
			}
			s.ScreenOffDrop = drop
			dc = true
		}
	}
	if !version || !bt || !dc {
		return s, errors.New("battery-format-unsupported")
	}
	return s, nil
}

// Only the bounded aggregate survives the native worker, never UID/package rows.
func (AndroidRunner) BatteryStats(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "/system/bin/dumpsys", "batterystats", "--checkin")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", err
	}
	b, readErr := io.ReadAll(io.LimitReader(pipe, maxBatteryBytes+1))
	if readErr != nil || len(b) > maxBatteryBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if len(b) > maxBatteryBytes {
			return "", errors.New("battery-response-too-large")
		}
		return "", errors.New("battery-read-failed")
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return "", errors.New("battery-timeout")
	}
	if err != nil {
		return "", errors.New("battery-read-failed")
	}
	return string(b), nil
}

type BatteryJob struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Boot    string    `json:"boot"`
	Started time.Time `json:"started"`
	PID     int       `json:"pid"`
	Error   string    `json:"error,omitempty"`
}

func (a *App) batterySnapshotView() any {
	var snapshot BatterySnapshot
	if readJSON(a.state("battery-snapshot.json"), &snapshot) != nil {
		return map[string]any{"state": "unavailable"}
	}
	snapshot.Rate = nil
	if snapshot.Boot == a.bootID() && time.Since(snapshot.At) >= 0 && time.Since(snapshot.At) <= 5*time.Minute && a.readSystem("/sys/class/power_supply/battery/status") == "Discharging" && snapshot.ScreenOffSeconds > 0 && snapshot.ScreenOffDrop >= 1 {
		rate := snapshot.ScreenOffDrop * 3600 / snapshot.ScreenOffSeconds
		snapshot.Rate = &rate
	}
	return snapshot
}
func (a *App) batteryJobStatus(id string) (map[string]any, error) {
	u, err := a.lock("battery-job")
	if err != nil {
		return nil, err
	}
	defer u()
	var j BatteryJob
	if err = readJSON(a.state("battery-job.json"), &j); os.IsNotExist(err) {
		return map[string]any{"state": "idle", "result": a.batterySnapshotView()}, nil
	}
	if err != nil {
		return nil, err
	}
	if id != "" && id != j.ID {
		return nil, errors.New("battery-job-not-found")
	}
	if (j.State == "queued" || j.State == "running") && (j.Boot != a.bootID() || time.Since(j.Started) < 0 || time.Since(j.Started) > 15*time.Second || (j.PID > 0 && !processAlive(j.PID))) {
		j.State = "failed"
		j.Error = "battery-job-interrupted"
		if err = atomicJSON(a.state("battery-job.json"), j); err != nil {
			return nil, err
		}
	}
	return map[string]any{"id": j.ID, "state": j.State, "error": j.Error, "result": a.batterySnapshotView()}, nil
}
func (a *App) startBatteryJob(force bool, launch func(string) error) (map[string]any, error) {
	if !a.ownerUser(context.Background()) {
		return nil, errors.New("owner-user-only")
	}
	u, err := a.lock("battery-job")
	if err != nil {
		return nil, err
	}
	defer u()
	var old BatteryJob
	_ = readJSON(a.state("battery-job.json"), &old)
	if old.Boot == a.bootID() && (old.State == "queued" || old.State == "running") && time.Since(old.Started) >= 0 && time.Since(old.Started) < 15*time.Second {
		return map[string]any{"id": old.ID, "state": old.State}, nil
	}
	var cached BatterySnapshot
	if !force && readJSON(a.state("battery-snapshot.json"), &cached) == nil && cached.Boot == a.bootID() && time.Since(cached.At) >= 0 && time.Since(cached.At) < 5*time.Minute {
		return map[string]any{"state": "completed", "result": a.batterySnapshotView()}, nil
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	j := BatteryJob{ID: hex.EncodeToString(random[:]), State: "queued", Boot: a.bootID(), Started: time.Now().UTC()}
	if err = atomicJSON(a.state("battery-job.json"), j); err != nil {
		return nil, err
	}
	if err = launch(j.ID); err != nil {
		j.State = "failed"
		j.Error = "battery-worker-unavailable"
		if err = atomicJSON(a.state("battery-job.json"), j); err != nil {
			return nil, err
		}
	}
	return map[string]any{"id": j.ID, "state": j.State, "error": j.Error}, nil
}
func (a *App) launchBatteryWorker(id string) error {
	return startDetachedCommand(filepath.Join(a.Paths.Module, "bin", "houjictl"), "battery-worker", id)
}
func (a *App) RunBatteryJob(ctx context.Context, id string) error {
	worker, err := a.lock("battery-worker")
	if err != nil {
		return err
	}
	defer worker()
	u, err := a.lock("battery-job")
	if err != nil {
		return err
	}
	var j BatteryJob
	if readJSON(a.state("battery-job.json"), &j) != nil || j.ID != id || j.State != "queued" || j.Boot != a.bootID() || time.Since(j.Started) > 15*time.Second || a.moduleDisabled() {
		u()
		return errors.New("battery-job-not-queued")
	}
	j.State = "running"
	j.PID = os.Getpid()
	err = atomicJSON(a.state("battery-job.json"), j)
	u()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var raw string
	if runner, ok := a.Runner.(interface {
		BatteryStats(context.Context) (string, error)
	}); ok {
		raw, err = runner.BatteryStats(ctx)
	} else {
		raw, err = a.Runner.Run(ctx, "dumpsys", "batterystats", "--checkin")
	}
	var snapshot BatterySnapshot
	if err == nil && ctx.Err() == nil {
		snapshot, err = parseBatteryStats(raw)
	} else if ctx.Err() != nil {
		err = errors.New("battery-timeout")
	}
	u, e := a.lock("battery-job")
	if e != nil {
		return e
	}
	defer u()
	var current BatteryJob
	if readJSON(a.state("battery-job.json"), &current) != nil || current.ID != id || current.Boot != a.bootID() {
		return errors.New("battery-job-interrupted")
	}
	if a.moduleDisabled() {
		err = errors.New("battery-job-interrupted")
	}
	if err != nil {
		current.State = "failed"
		current.Error = "battery-read-failed"
		if strings.HasPrefix(err.Error(), "battery-") {
			current.Error = err.Error()
		}
	} else {
		snapshot.At = time.Now().UTC()
		snapshot.Boot = a.bootID()
		if err = atomicJSON(a.state("battery-snapshot.json"), snapshot); err != nil {
			return err
		}
		current.State = "completed"
	}
	current.PID = 0
	return atomicJSON(a.state("battery-job.json"), current)
}
