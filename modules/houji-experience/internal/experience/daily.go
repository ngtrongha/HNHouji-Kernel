package experience

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

type DailyAttempt struct {
	Day       string `json:"day"`
	At        string `json:"at"`
	Automatic bool   `json:"automatic"`
}
type DailyResult struct {
	At        string         `json:"at,omitempty"`
	Automatic bool           `json:"automatic"`
	Result    map[string]any `json:"result,omitempty"`
	Legend    []HabitApp     `json:"legend,omitempty"`
}

// This is deliberately constructed, never a redaction of the private history.
// App IDs are local labels for this payload, not cross-day identities.
func (a *App) dailyPayload(at time.Time) (map[string]any, error) {
	payload, _, err := a.dailySnapshot(at)
	return payload, err
}
func (a *App) dailySnapshot(at time.Time) (map[string]any, []HabitApp, error) {
	c, err := a.learningConfig()
	if err != nil {
		return nil, nil, err
	}
	h, err := a.usageHistory()
	if err != nil {
		return nil, nil, err
	}
	l := Learner{History: h, Config: c}
	l.day(at) // prune stale/future days before sharing
	core, err := a.config()
	if err != nil {
		return nil, nil, err
	}
	apps := []map[string]any{}
	legend := historyApps(l.History, core)
	for _, item := range legend {
		profile := item.Profile
		source := "automatic"
		if profile != "" {
			source = "manual"
			if core.AIProfiles[item.Package] == profile {
				source = "ai"
			}
		}
		if profile == "" {
			profile = "automatic"
		}
		apps = append(apps, map[string]any{"id": item.ID, "seconds": item.Seconds, "sessions": item.Sessions, "days": item.Days, "performance_supported": item.GameSupported, "profile": profile, "rule_source": source})
	}
	days := map[string]any{}
	var samples, observed int64
	for date, day := range l.History.Days {
		samples += day.Samples
		observed += day.Observed
		days[date] = map[string]any{"timezone": day.Timezone, "observed_seconds": day.Observed, "active_seconds": day.Active, "samples": day.Samples, "charging_samples": day.ChargingSamples, "hours": day.Hours, "metrics": day.Metrics}
	}
	return map[string]any{"device": "Xiaomi 14 houji 8GB", "through": h.Through, "timezone": c.Timezone, "retention_days": 7, "samples": samples, "observed_seconds": observed, "coverage_note": "Observed intervals only; gaps and missing sensors are unknown. Max 16 apps, 32 tracked per day; app IDs are temporary labels. Not a battery/FPS benchmark.", "days": days, "apps": apps, "settings": map[string]any{"adaptive": c.Adaptive, "kernel_preset": core.KernelTuning, "automatic_application": c.AutoApply}, "module_controls": "app_profile: automatic/daily/native-game/windows-game/linux; native-game only when supported. kernel_preset: master on/off; kernel_control: swap_readahead/tcp_congestion/data_fsync on/off with fixed existing targets only when supported and kernel matches. No custom knob values or commands. Apply only through module validation."}, legend, nil
}

const dailyInstruction = usefulAdvisorInstruction + `
Đây là thống kê tối đa 7 ngày theo giờ và ứng dụng mã hóa tên. Sum/count là trung bình các mẫu, không phải tích phân theo thời gian. Chỉ đánh giá phần thời gian được quan sát; không suy đoán thói quen từ khoảng thiếu dữ liệu. CPU busy là phần trăm từ chênh lệch /proc/stat.
Nêu các khung giờ nổi bật, thay đổi giữa các ngày có đủ độ phủ và những đề xuất đáng thử theo thứ tự ưu tiên. Chỉ tạo actions cho app_profile, kernel_preset hoặc kernel_control đã có trong snapshot và schema. Native-game chỉ khi performance_supported=true. Không tạo tham số kernel mới hoặc tuyên bố đã áp dụng. Liên hệ app-ID để người dùng đối chiếu bảng trên máy. Khi chưa đủ 2 ngày có mẫu, giải thích mẫu hiện có và đề xuất thu thập thêm, không khẳng định đã học được thói quen.`

func (a *App) dailyAnalyze(ctx context.Context, at time.Time, automatic bool, tr http.RoundTripper) (DailyResult, error) {
	unlock, err := a.lock("daily")
	if err != nil {
		return DailyResult{}, err
	}
	defer unlock()
	c, err := a.learningConfig()
	if err != nil {
		return DailyResult{}, err
	}
	core, err := a.config()
	if err != nil {
		return DailyResult{}, err
	}
	baseCore := core
	if !c.Enabled || !core.Enabled || a.moduleDisabled() || !a.ownerUser(ctx) {
		return DailyResult{}, errors.New("learning-disabled")
	}
	var ai AIConfig
	if readJSON(a.state("ai.json"), &ai) != nil || ai.validate() != nil || !ai.Enabled {
		return DailyResult{}, errors.New("ai-disabled-or-not-configured")
	}
	location, _ := time.LoadLocation(c.Timezone)
	local := at.In(location)
	date := local.Format("2006-01-02")
	if automatic {
		if !c.DailyAI || local.Hour() < c.Hour {
			return DailyResult{}, errors.New("daily-not-due")
		}
		var previous DailyAttempt
		if e := readJSON(a.state("daily-attempt.json"), &previous); e != nil && !os.IsNotExist(e) {
			return DailyResult{}, errors.New("daily-attempt-invalid")
		} else if previous.Day >= date {
			return DailyResult{}, errors.New("daily-already-attempted")
		}
		if c.ChargingOnly {
			status := a.readSystem("/sys/class/power_supply/battery/status")
			if status != "Charging" && status != "Full" {
				return DailyResult{}, errors.New("daily-waiting-for-charge")
			}
		}
	}
	u, err := a.lock("learning")
	if err != nil {
		return DailyResult{}, err
	}
	payload, legend, err := a.dailySnapshot(at)
	if err != nil {
		u()
		return DailyResult{}, err
	}
	u()
	if payload["samples"].(int64) < 2 || payload["observed_seconds"].(int64) < 60 {
		return DailyResult{}, errors.New("learning-needs-more-samples")
	}
	payload["daily_comparison"] = comparisonRows(payload["days"].(map[string]any))
	payload["automatic_run"] = automatic
	sync := a.KernelSync(ctx)
	payload["kernel_compatibility"] = map[string]any{"ready": sync.Ready, "state": sync.State}
	knobs := a.configuredKernelPlan(baseCore)
	payload["kernel_controls"] = knobs
	// Reserve before the network, including failures. No unattended retries.
	if err = atomicJSON(a.state("daily-attempt.json"), DailyAttempt{Day: date, At: at.UTC().Format(time.RFC3339), Automatic: automatic}); err != nil {
		return DailyResult{}, err
	}
	result := DailyResult{At: at.UTC().Format(time.RFC3339), Automatic: automatic, Legend: legend, Result: a.requestAI(ctx, "", false, dailyInstruction, pretty(payload), proposalSchema(legend, baseCore, knobs, sync.Ready, automatic), tr)}
	if ctx.Err() != nil {
		return DailyResult{}, ctx.Err()
	}
	u, err = a.lock("learning")
	if err != nil {
		return result, err
	}
	current, err := a.learningConfig()
	if err != nil {
		u()
		return result, err
	}
	core, err = a.config()
	if err != nil {
		u()
		return result, err
	}
	if !current.Enabled || current.Generation != c.Generation || !core.Enabled || a.moduleDisabled() {
		u()
		return DailyResult{}, errors.New("learning-changed-during-analysis")
	}
	u()
	result.Result = a.attachProposal(result.Result, legend, baseCore, c.Generation, time.Now())
	if automatic && c.AutoApply {
		if p, ok := result.Result["proposal"].(AIProposal); ok {
			application, applyErr := a.applyProposal(ctx, p.ID, true)
			if applyErr != nil {
				result.Result["application"] = map[string]any{"status": "skipped", "error": applyErr.Error()}
			} else {
				result.Result["application"] = application
			}
		} else {
			result.Result["application"] = map[string]any{"status": "skipped", "error": "proposal-no-valid-actions"}
		}
	}
	u, err = a.lock("learning")
	if err != nil {
		return result, err
	}
	defer u()
	current, err = a.learningConfig()
	if err != nil {
		return result, err
	}
	if current.Generation != c.Generation {
		return DailyResult{}, errors.New("learning-changed-during-analysis")
	}
	if err = atomicJSON(a.state("daily-ai.json"), result); err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) maybeDailyAI(ctx context.Context, at time.Time) {
	if !a.dailyChecked.IsZero() && at.Sub(a.dailyChecked) < time.Minute {
		return
	}
	a.dailyChecked = at
	c, err := a.learningConfig()
	if err != nil {
		return
	}
	purge := a.dailyPurgeAt.IsZero() || at.Sub(a.dailyPurgeAt) >= time.Hour
	if !purge && (!c.Enabled || !c.DailyAI) {
		return
	}
	if !a.dailyBusy.CompareAndSwap(false, true) {
		return
	}
	// A separate App owns no watcher state. Network I/O cannot block Step/restore.
	worker := New(a.Paths, AndroidRunner{})
	if purge {
		a.dailyPurgeAt = at
	}
	go func() {
		defer a.dailyBusy.Store(false)
		if purge {
			_ = worker.purgeLearning(at)
		}
		if c.Enabled && c.DailyAI {
			_, _ = worker.dailyAnalyze(ctx, at, true, nil)
		}
	}()
}

func (a *App) saveLearning(c LearningConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	unlock, err := a.lock("learning")
	if err != nil {
		return err
	}
	defer unlock()
	old, err := a.learningConfig()
	if err != nil {
		return err
	}
	c.Generation = old.Generation + 1
	return atomicJSON(a.state("learning-config.json"), c)
}
func (a *App) resetLearning() error {
	_, _ = a.AIJobStatus("", true)
	unlock, err := a.lock("daily")
	if err != nil {
		return err
	}
	defer unlock()
	u, err := a.lock("learning")
	if err != nil {
		return err
	}
	defer u()
	c, err := a.learningConfig()
	if err != nil {
		return err
	}
	c.Generation++
	if err = atomicJSON(a.state("learning-config.json"), c); err != nil {
		return err
	}
	jobUnlock, err := a.lock("ai-job")
	if err != nil {
		return err
	}
	defer jobUnlock()
	for _, name := range []string{"learning.json", "daily-ai.json", "ai-job.json", "ai-proposal.json", "battery-snapshot.json", "battery-job.json"} {
		if err = os.Remove(a.state(name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Keep today's attempt marker: clearing history does not buy an automatic retry.
	return nil
}
func (a *App) clearLearning() {
	u, err := a.lock("learning")
	if err != nil {
		return
	}
	defer u()
	v, err := a.lock("ai-job")
	if err != nil {
		return
	}
	defer v()
	for _, name := range []string{"learning-config.json", "learning.json", "daily-ai.json", "daily-attempt.json", "ai-job.json", "ai-proposal.json", "ai-application.json", "battery-snapshot.json", "battery-job.json", "settings-preview.json", "settings-custom.json"} {
		_ = os.Remove(a.state(name))
	}
}

func (a *App) purgeLearning(at time.Time) error {
	u, err := a.lock("daily")
	if err != nil {
		return err
	}
	defer u()
	v, err := a.lock("learning")
	if err != nil {
		return err
	}
	defer v()
	c, err := a.learningConfig()
	if err != nil {
		return err
	}
	h, err := a.usageHistory()
	if err != nil {
		return err
	}
	if pruneHistory(&h, c, at) {
		if err = atomicJSON(a.state("learning.json"), h); err != nil {
			return err
		}
	}
	jobUnlock, err := a.lock("ai-job")
	if err != nil {
		return err
	}
	var job AIJob
	if readAIJSON(a.state("ai-job.json"), &job) == nil && !activeJob(job) && job.Started.Before(at.Add(-7*24*time.Hour)) {
		_ = os.Remove(a.state("ai-job.json"))
	}
	jobUnlock()
	var daily DailyResult
	if readAIJSON(a.state("daily-ai.json"), &daily) == nil {
		date, e := time.Parse(time.RFC3339, daily.At)
		if e != nil || date.Before(at.Add(-7*24*time.Hour)) || date.After(at.Add(time.Minute)) {
			return os.Remove(a.state("daily-ai.json"))
		}
	}
	return nil
}
