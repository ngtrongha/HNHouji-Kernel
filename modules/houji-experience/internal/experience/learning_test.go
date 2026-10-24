package experience

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func atLearning() time.Time { return time.Date(2026, 10, 25, 21, 0, 0, 0, time.UTC) }
func seedUsage(t *testing.T, a *App, at time.Time) UsageHistory {
	t.Helper()
	h := UsageHistory{Version: 1, Days: map[string]*UsageDay{}}
	for i := 0; i < 2; i++ {
		var hours [24]int64
		hours[at.Hour()] = 700
		h.Days[at.AddDate(0, 0, -i).Format("2006-01-02")] = &UsageDay{Timezone: "UTC", Observed: 1200, Active: 1200, Samples: 20, Metrics: map[string]MetricSummary{"cpu_busy_percent": {Count: 20, Sum: 600, Max: 60}}, Apps: map[string]*AppHabit{"com.example.game": {Seconds: 1200, Sessions: 2, Hours: hours, GameSupported: true, GameChecked: true}}}
	}
	must(t, atomicJSON(a.state("learning.json"), h))
	return h
}
func TestObserveGapsMidnightSamplingAndReset(t *testing.T) {
	a, _ := fixture(t)
	ctx := context.Background()
	at := atLearning().Add(3*time.Hour - time.Second*30)
	must(t, atomicWrite(a.system("/proc/stat"), []byte("cpu 100 0 100 800 0 0 0 0\n"), 0600))
	must(t, a.Observe(ctx, "com.example.game", false, at))
	must(t, a.Observe(ctx, "com.example.game", false, at.Add(60*time.Second)))
	l := a.learner
	if len(l.History.Days) != 2 || l.History.Days["2026-10-25"].Active != 30 || l.History.Days["2026-10-26"].Active != 30 {
		t.Fatal(l.History)
	}
	if !l.History.Days["2026-10-26"].Apps["com.example.game"].GameSupported {
		t.Fatal("game capability not learned")
	}
	must(t, a.Observe(ctx, "com.example.game", false, at.Add(10*time.Minute)))
	if l.History.Days["2026-10-26"].Active != 30 {
		t.Fatal("unknown gap counted")
	}
	must(t, a.flushLearning(l, at.Add(10*time.Minute)))
	must(t, a.resetLearning())
	must(t, a.flushLearning(l, at.Add(11*time.Minute)))
	if exists(a.state("learning.json")) {
		t.Fatal("stale watcher resurrected reset data")
	}
	must(t, a.Observe(ctx, "com.example.game", false, at.Add(11*time.Minute)))
	if a.learner.History.Days["2026-10-26"].Active != 0 {
		t.Fatal("reset did not invalidate memory")
	}
}
func TestLearningPausedAndCPU(t *testing.T) {
	a, f := fixture(t)
	c := defaultLearningConfig()
	c.Enabled = false
	must(t, a.saveLearning(c))
	must(t, a.Observe(context.Background(), "com.example.game", false, atLearning()))
	if a.learner != nil || exists(a.state("learning.json")) || f.writes != 0 {
		t.Fatal("tracked while disabled")
	}
	before, _ := cpuCounters("cpu 10 0 10 80 0 0 0 0 0 0\n")
	after, _ := cpuCounters("cpu 20 0 30 150 0 0 0 0 0 0\n")
	if n, ok := cpuPercent(before, after); !ok || n != 30 {
		t.Fatal(n, ok)
	}
	if _, ok := cpuPercent(after, before); ok {
		t.Fatal("accepted backwards counters")
	}
}

func TestRetentionWhenPausedAndInFlightOptOut(t *testing.T) {
	a, _ := fixture(t)
	at := atLearning()
	seedUsage(t, a, at)
	must(t, atomicJSON(a.state("daily-ai.json"), DailyResult{At: at.AddDate(0, 0, -8).Format(time.RFC3339), Legend: []HabitApp{{Package: "com.example.game"}}}))
	c := defaultLearningConfig()
	c.Enabled = false
	must(t, a.saveLearning(c))
	must(t, a.purgeLearning(at.AddDate(0, 0, 8)))
	h, err := a.usageHistory()
	must(t, err)
	if len(h.Days) != 0 || exists(a.state("daily-ai.json")) {
		t.Fatal("paused data exceeded retention")
	}
	c.Enabled = true
	must(t, a.saveLearning(c))
	seedUsage(t, a, at)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
	tr := transportFunc(func(*http.Request) (*http.Response, error) {
		c.Enabled = false
		must(t, a.saveLearning(c))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Advice"}]}]}`)), Header: http.Header{}}, nil
	})
	if _, err = a.dailyAnalyze(context.Background(), at, false, tr); err == nil || exists(a.state("daily-ai.json")) {
		t.Fatal("saved result after opt out", err)
	}
}
func TestAdaptiveEvidenceManualRulesAndPressureRestore(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	now := atLearning()
	h := seedUsage(t, a, now)
	a.learner = &Learner{History: h, Config: defaultLearningConfig(), SessionAt: now.Add(-time.Minute), SampleAt: now, LastMetrics: map[string]any{"cpu_busy_percent": float64(30), "psi_full_avg10": float64(0), "thermal_status": float64(0)}}
	p, reason := a.adaptiveProfile(ctx, "daily", "com.example.game", false, now)
	if p != "native-game" || reason != "adaptive-learned-game" {
		t.Fatal(p, reason)
	}
	c, _ := a.config()
	c.Apps["com.example.game"] = "daily"
	must(t, atomicJSON(a.state("config.json"), c))
	if p, _ = a.adaptiveProfile(ctx, "daily", "com.example.game", false, now); p != "daily" {
		t.Fatal("overrode manual daily")
	}
	delete(c.Apps, "com.example.game")
	must(t, atomicJSON(a.state("config.json"), c))
	a.learner.SampleAt = now.Add(-3 * time.Minute)
	if p, _ = a.adaptiveProfile(ctx, "daily", "com.example.game", false, now); p != "daily" {
		t.Fatal("used stale sensors")
	}
	configured(t, a, false)
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "performance" {
		t.Fatal(f.game)
	}
	a.learner.PressureUntil = time.Now().Add(time.Minute)
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "standard" {
		t.Fatal("pressure did not restore original game mode")
	}
	j, _ := a.journal()
	for _, entry := range j.Entries {
		if entry.Kind == "game" {
			t.Fatal("game override still owned")
		}
	}
}

func TestLearnedProfileUsesJournalAndRestoresOnSleep(t *testing.T) {
	a, f := fixture(t)
	now := time.Now().UTC()
	h := seedUsage(t, a, now)
	a.learner = &Learner{History: h, Config: defaultLearningConfig(), SessionAt: now.Add(-time.Minute), SampleAt: now, LastMetrics: map[string]any{"cpu_busy_percent": float64(30), "psi_full_avg10": float64(0), "thermal_status": float64(0)}}
	must(t, a.Step(context.Background(), "com.example.game", false))
	if f.game != "performance" {
		t.Fatal("learned profile not applied", f.game)
	}
	var runtime Runtime
	must(t, readJSON(a.state("runtime.json"), &runtime))
	if runtime.Reason != "adaptive-learned-game" {
		t.Fatal(runtime)
	}
	must(t, a.Step(context.Background(), "", true))
	if f.game != "standard" {
		t.Fatal("learned profile not restored on sleep")
	}
}
func TestLearningRetentionAndCorruptHistory(t *testing.T) {
	a, _ := fixture(t)
	h := seedUsage(t, a, atLearning())
	l := Learner{History: h, Config: defaultLearningConfig()}
	l.day(atLearning().AddDate(0, 0, 8))
	if len(l.History.Days) != 1 {
		t.Fatal(l.History)
	}
	h.Days = map[string]*UsageDay{"2026-10-25": {Observed: -1, Apps: map[string]*AppHabit{}, Metrics: map[string]MetricSummary{}}}
	must(t, atomicJSON(a.state("learning.json"), h))
	if _, err := a.usageHistory(); err == nil {
		t.Fatal("accepted negative history")
	}
	if err := a.Observe(context.Background(), "com.example.game", false, atLearning()); err == nil || a.learner != nil {
		t.Fatal("corrupt history was used")
	}
}
func TestDailyPrivacyReservationAndNoAutomaticRetry(t *testing.T) {
	a, f := fixture(t)
	at := atLearning()
	seedUsage(t, a, at)
	ctx := context.Background()
	c := defaultLearningConfig()
	c.DailyAI = true
	must(t, a.saveLearning(c))
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	preview, err := a.dailyPayload(at)
	must(t, err)
	if strings.Contains(pretty(preview), "com.example") || strings.Contains(pretty(preview), "test-secret") {
		t.Fatal("private preview")
	}
	calls := 0
	tr := transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), "com.example") || strings.Contains(string(body), "test-secret") {
			t.Fatal("private outbound payload")
		}
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("no")), Header: http.Header{}}, nil
	})
	d, err := a.dailyAnalyze(ctx, at, true, tr)
	must(t, err)
	if d.Result["ai_error"] != "http-429" || len(d.Legend) != 1 {
		t.Fatal(d)
	}
	if _, err = a.dailyAnalyze(ctx, at.Add(time.Minute), true, tr); err == nil || calls != 1 {
		t.Fatal("retried after failure", err, calls)
	}
	if f.writes != 0 {
		t.Fatal("AI changed settings")
	}
	var stored DailyResult
	must(t, readJSON(a.state("daily-ai.json"), &stored))
	if strings.Contains(pretty(stored), "test-secret") {
		t.Fatal("stored secret")
	}
	must(t, a.resetLearning())
	if !exists(a.state("daily-attempt.json")) {
		t.Fatal("reset erased retry protection")
	}
}
func TestDailyGatesTimezoneAndManualReviewOnly(t *testing.T) {
	a, _ := fixture(t)
	at := atLearning()
	ctx := context.Background()
	seedUsage(t, a, at)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	calls := 0
	tr := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"app-01: xem xét daily; $(reboot)"}}]}`)), Header: http.Header{}}, nil
	})
	if _, err := a.dailyAnalyze(ctx, at, true, tr); err == nil || calls != 0 {
		t.Fatal("default enabled unattended AI")
	}
	c := defaultLearningConfig()
	c.DailyAI = true
	c.Timezone = "Asia/Ho_Chi_Minh"
	c.Hour = 5
	must(t, a.saveLearning(c))
	// UTC 21:00 is local 04:00 tomorrow: not due yet.
	if _, err := a.dailyAnalyze(ctx, at, true, tr); err == nil || calls != 0 {
		t.Fatal("ignored timezone hour")
	}
	c.ChargingOnly = true
	c.Hour = 4
	must(t, a.saveLearning(c))
	if _, err := a.dailyAnalyze(ctx, at, true, tr); err == nil || calls != 0 {
		t.Fatal("ignored charging gate")
	}
	result, err := a.dailyAnalyze(ctx, at, false, tr)
	must(t, err)
	if calls != 1 || result.Result["source"] != "api" {
		t.Fatal(result, calls)
	}
	core, _ := a.config()
	if len(core.Apps) != 0 {
		t.Fatal("AI applied rules")
	}
	c.Enabled = false
	must(t, a.saveLearning(c))
	if _, err = a.dailyAnalyze(ctx, at.AddDate(0, 0, 1), false, tr); err == nil || calls != 1 {
		t.Fatal("ignored opt out")
	}
	a.clearLearning()
	for _, file := range []string{"learning.json", "learning-config.json", "daily-ai.json", "daily-attempt.json"} {
		if _, err = os.Stat(a.state(file)); !os.IsNotExist(err) {
			t.Fatal("retained", file)
		}
	}
}
