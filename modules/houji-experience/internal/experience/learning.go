package experience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

// Private, bounded statistical history; no model, notification content or URLs.
type LearningConfig struct {
	Enabled      bool   `json:"enabled"`
	Adaptive     bool   `json:"adaptive"`
	DailyAI      bool   `json:"daily_ai"`
	Hour         int    `json:"hour"`
	ChargingOnly bool   `json:"charging_only"`
	Timezone     string `json:"timezone"`
	Generation   int64  `json:"generation"`
}

func defaultLearningConfig() LearningConfig {
	return LearningConfig{Enabled: true, Adaptive: true, Hour: 21, Timezone: "UTC"}
}
func (a *App) learningConfig() (LearningConfig, error) {
	c := defaultLearningConfig()
	err := readJSON(a.state("learning-config.json"), &c)
	if os.IsNotExist(err) {
		err = nil
	}
	if err == nil {
		err = c.validate()
	}
	return c, err
}
func (c LearningConfig) validate() error {
	if c.Hour < 0 || c.Hour > 23 || c.Timezone == "" || len(c.Timezone) > 100 {
		return errors.New("invalid daily hour or timezone")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return errors.New("unknown timezone")
	}
	return nil
}
func (a *App) initLearning(ctx context.Context) error {
	if exists(a.state("learning-config.json")) {
		return nil
	}
	c := defaultLearningConfig()
	zone, err := a.run(ctx, "getprop", "persist.sys.timezone")
	if err == nil {
		if _, err = time.LoadLocation(zone); err == nil {
			c.Timezone = zone
		}
	}
	return atomicJSON(a.state("learning-config.json"), c)
}

type MetricSummary struct {
	Count int64   `json:"count"`
	Sum   float64 `json:"sum"`
	Max   float64 `json:"max"`
}
type HourSummary struct {
	Observed int64 `json:"observed_seconds"`
	Active   int64 `json:"active_seconds"`
	Heavy    int64 `json:"heavy_samples"`
	Samples  int64 `json:"samples"`
}
type AppHabit struct {
	Seconds       int64     `json:"seconds"`
	Sessions      int64     `json:"sessions"`
	Hours         [24]int64 `json:"hours"`
	GameSupported bool      `json:"game_supported"`
	GameChecked   bool      `json:"game_checked"`
}
type UsageDay struct {
	Timezone        string                   `json:"timezone"`
	Observed        int64                    `json:"observed_seconds"`
	Active          int64                    `json:"active_seconds"`
	ChargingSamples int64                    `json:"charging_samples"`
	Samples         int64                    `json:"samples"`
	Hours           [24]HourSummary          `json:"hours"`
	Metrics         map[string]MetricSummary `json:"metrics"`
	Apps            map[string]*AppHabit     `json:"apps"`
}
type UsageHistory struct {
	Version int                  `json:"version"`
	Through string               `json:"through,omitempty"`
	Days    map[string]*UsageDay `json:"days"`
}
type Learner struct {
	History                                             UsageHistory
	Config                                              LearningConfig
	LastAt, SampleAt, FlushAt, SessionAt, PressureUntil time.Time
	LastPackage                                         string
	LastAsleep                                          bool
	CPU                                                 [8]uint64
	CPUValid                                            bool
	LastMetrics                                         map[string]any
	Intensity                                           string
	Remainder                                           time.Duration
}

func (a *App) usageHistory() (UsageHistory, error) {
	h := UsageHistory{Version: 1, Days: map[string]*UsageDay{}}
	err := readJSON(a.state("learning.json"), &h)
	if os.IsNotExist(err) {
		err = nil
	}
	if err == nil && (h.Version != 1 || len(h.Days) > 7) {
		err = errors.New("invalid learning history")
	}
	if h.Days == nil {
		h.Days = map[string]*UsageDay{}
	}
	for key, day := range h.Days {
		if _, e := time.Parse("2006-01-02", key); e != nil || day == nil || len(day.Apps) > 32 || len(day.Metrics) > 20 {
			return h, errors.New("invalid learning day")
		}
		if day.Apps == nil {
			day.Apps = map[string]*AppHabit{}
		}
		if day.Metrics == nil {
			day.Metrics = map[string]MetricSummary{}
		}
		if day.Observed < 0 || day.Observed > 90000 || day.Active < 0 || day.Active > day.Observed || day.Samples < 0 || day.Samples > 1500 || day.ChargingSamples < 0 || day.ChargingSamples > day.Samples {
			return h, errors.New("invalid learning counters")
		}
		for _, m := range day.Metrics {
			if m.Count < 0 || m.Count > day.Samples || math.IsNaN(m.Sum) || math.IsInf(m.Sum, 0) || math.IsNaN(m.Max) || math.IsInf(m.Max, 0) {
				return h, errors.New("invalid learning metrics")
			}
		}
		for pkg, entry := range day.Apps {
			if !validPackage(pkg) || entry == nil || entry.Seconds < 0 || entry.Seconds > day.Active || entry.Sessions < 0 || entry.Sessions > 30000 {
				return h, errors.New("invalid learning app")
			}
			for _, s := range entry.Hours {
				if s < 0 || s > entry.Seconds {
					return h, errors.New("invalid app hours")
				}
			}
		}
	}
	return h, err
}
func (l *Learner) day(at time.Time) *UsageDay {
	location, _ := time.LoadLocation(l.Config.Timezone)
	at = at.In(location)
	key := at.Format("2006-01-02")
	pruneHistory(&l.History, l.Config, at)
	d := l.History.Days[key]
	if d == nil || d.Timezone != l.Config.Timezone {
		d = &UsageDay{Timezone: l.Config.Timezone, Metrics: map[string]MetricSummary{}, Apps: map[string]*AppHabit{}}
		l.History.Days[key] = d
	}
	return d
}
func pruneHistory(h *UsageHistory, c LearningConfig, at time.Time) bool {
	location, _ := time.LoadLocation(c.Timezone)
	at = at.In(location)
	key := at.Format("2006-01-02")
	changed := false
	for old := range h.Days {
		if old < at.AddDate(0, 0, -6).Format("2006-01-02") || old > key {
			delete(h.Days, old)
			changed = true
		}
	}
	return changed
}
func (a *App) flushLearning(l *Learner, at time.Time) error {
	unlock, err := a.lock("learning")
	if err != nil {
		return err
	}
	defer unlock()
	// A settings save/reset invalidates a watcher's in-memory history.
	c, err := a.learningConfig()
	if err != nil {
		return err
	}
	if c.Generation != l.Config.Generation {
		return nil
	}
	core, err := a.config()
	if err != nil {
		return err
	}
	if !core.Enabled || a.moduleDisabled() {
		return nil
	}
	pruneHistory(&l.History, c, at)
	l.History.Through = at.UTC().Format(time.RFC3339)
	if err = atomicJSON(a.state("learning.json"), l.History); err == nil {
		l.FlushAt = at
	}
	return err
}
func cpuCounters(raw string) ([8]uint64, bool) {
	var counters [8]uint64
	lines := strings.SplitN(raw, "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		return counters, false
	}
	for i := range counters {
		v, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return counters, false
		}
		counters[i] = v
	}
	return counters, true
}
func cpuPercent(before, after [8]uint64) (float64, bool) {
	var total, idle uint64
	for i := range before {
		if after[i] < before[i] {
			return 0, false
		}
		delta := after[i] - before[i]
		total += delta
		if i == 3 || i == 4 {
			idle += delta
		}
	}
	if total == 0 {
		return 0, false
	}
	return 100 * float64(total-idle) / float64(total), true
}
func (a *App) gameSupported(ctx context.Context, pkg string) bool {
	if !validPackage(pkg) {
		return false
	}
	out, err := a.run(ctx, "cmd", "game", "list-modes", pkg)
	_, modes, ok := strings.Cut(out, "available game modes: [")
	return err == nil && ok && strings.Contains(strings.SplitN(modes, "]", 2)[0], "performance")
}

// Observe runs in the watcher only. Accounting stays in memory between 5-minute
// flushes. /proc CPU deltas and Report metrics are sampled at most once/minute.
func (a *App) Observe(ctx context.Context, pkg string, asleep bool, at time.Time) error {
	c, err := a.learningConfig()
	if err != nil {
		a.learner = nil
		return err
	}
	core, err := a.config()
	if err != nil {
		return err
	}
	if !c.Enabled || !core.Enabled || a.moduleDisabled() || !a.ownerUser(ctx) {
		if a.learner != nil {
			a.learner.LastAt = time.Time{}
			a.learner.LastPackage = ""
			a.learner.SampleAt = time.Time{}
			a.learner.CPUValid = false
			a.learner.Remainder = 0
		}
		return nil
	}
	if a.learner == nil || a.learner.Config.Generation != c.Generation {
		h, err := a.usageHistory()
		if err != nil {
			a.learner = nil
			return err
		}
		a.learner = &Learner{History: h, Config: c, Intensity: "unknown"}
	}
	l := a.learner
	location, _ := time.LoadLocation(c.Timezone)
	at = at.In(location)
	if !validPackage(pkg) || asleep {
		pkg = ""
	}
	if !l.LastAt.IsZero() {
		delta := at.Sub(l.LastAt)
		// Unknown/suspended gaps are not counted as known app usage.
		if delta > 0 && delta <= 90*time.Second {
			// Split a known interval at hour/day boundaries; never fill unknown gaps.
			cursor := l.LastAt.In(location)
			for cursor.Before(at) {
				end := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), cursor.Hour(), 0, 0, 0, location).Add(time.Hour)
				if !end.After(cursor) {
					end = cursor.Add(time.Minute)
				}
				if end.After(at) {
					end = at
				}
				duration := end.Sub(cursor) + l.Remainder
				seconds := int64(duration / time.Second)
				l.Remainder = duration % time.Second
				d := l.day(cursor)
				hour := cursor.Hour()
				d.Observed += seconds
				d.Hours[hour].Observed += seconds
				if !l.LastAsleep {
					d.Active += seconds
					d.Hours[hour].Active += seconds
				}
				if !l.LastAsleep && l.LastPackage != "" {
					entry := d.Apps[l.LastPackage]
					if entry == nil && len(d.Apps) < 32 {
						entry = &AppHabit{}
						d.Apps[l.LastPackage] = entry
					}
					if entry != nil {
						entry.Seconds += seconds
						entry.Hours[hour] += seconds
					}
				}
				cursor = end
			}
		} else {
			l.LastPackage = ""
			l.CPUValid = false
			l.SampleAt = time.Time{}
			l.Remainder = 0
		}
	}
	day := l.day(at)
	if pkg != "" {
		if day.Apps[pkg] == nil && len(day.Apps) < 32 {
			day.Apps[pkg] = &AppHabit{}
		}
		if entry := day.Apps[pkg]; entry != nil {
			if l.LastPackage != pkg {
				entry.Sessions++
				l.SessionAt = at
			}
			if !entry.GameChecked && at.Sub(l.SessionAt) >= 30*time.Second {
				entry.GameSupported = a.gameSupported(ctx, pkg)
				entry.GameChecked = true
			}
		}
	}
	l.LastAt = at
	l.LastPackage = pkg
	l.LastAsleep = asleep
	if l.SampleAt.IsZero() || at.Before(l.SampleAt) || at.Sub(l.SampleAt) >= time.Minute {
		sampleCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		report := a.Report(sampleCtx)
		cancel()
		metrics, _ := report["metrics"].(map[string]any)
		if metrics == nil {
			metrics = map[string]any{}
		}
		after, valid := cpuCounters(a.readSystem("/proc/stat"))
		if valid && l.CPUValid {
			if busy, ok := cpuPercent(l.CPU, after); ok {
				metrics["cpu_busy_percent"] = busy
			}
		}
		l.CPU = after
		l.CPUValid = valid
		day.Samples++
		day.Hours[at.Hour()].Samples++
		for _, key := range []string{"MemAvailable_kib", "cpu_busy_percent", "cpu_load_average_1m", "psi_some_avg10", "psi_full_avg10", "thermal_status", "battery_celsius", "zram_allocated_bytes"} {
			if value, ok := metrics[key].(float64); ok {
				m := day.Metrics[key]
				m.Count++
				m.Sum += value
				if m.Count == 1 || value > m.Max {
					m.Max = value
				}
				day.Metrics[key] = m
			}
		}
		charging := a.readSystem("/sys/class/power_supply/battery/status")
		if charging == "Charging" || charging == "Full" {
			day.ChargingSamples++
		}
		l.Intensity = "unknown"
		busy, busyOK := metrics["cpu_busy_percent"].(float64)
		full, fullOK := metrics["psi_full_avg10"].(float64)
		thermal, thermalOK := metrics["thermal_status"].(float64)
		if busyOK {
			l.Intensity = "light"
			if busy >= 25 {
				l.Intensity = "medium"
			}
			if busy >= 60 {
				l.Intensity = "heavy"
			}
		}
		if (fullOK && full >= 2) || (thermalOK && thermal >= 3) {
			l.PressureUntil = at.Add(2 * time.Minute)
			l.Intensity = "pressure"
		}
		if l.Intensity == "heavy" || l.Intensity == "pressure" {
			day.Hours[at.Hour()].Heavy++
		}
		l.LastMetrics = metrics
		l.SampleAt = at
	}
	if l.FlushAt.IsZero() || at.Sub(l.FlushAt) >= 5*time.Minute {
		return a.flushLearning(l, at)
	}
	return nil
}

func (a *App) adaptiveProfile(ctx context.Context, profile, pkg string, asleep bool, at time.Time) (string, string) {
	l := a.learner
	if l == nil || !l.Config.Enabled || !l.Config.Adaptive || asleep {
		return profile, ""
	}
	// An unreadable settings file or explicit opt-out takes effect immediately.
	c, err := a.learningConfig()
	if err != nil || !c.Enabled || !c.Adaptive || c.Generation != l.Config.Generation {
		return profile, ""
	}
	if at.Before(l.PressureUntil) && profile == "native-game" {
		return "daily", "adaptive-pressure"
	}
	if profile != "daily" || pkg == "" || at.Sub(l.SessionAt) < 30*time.Second || at.Before(l.PressureUntil) || l.SampleAt.IsZero() || at.Before(l.SampleAt) || at.Sub(l.SampleAt) > 2*time.Minute {
		return profile, ""
	}
	for _, key := range []string{"cpu_busy_percent", "psi_full_avg10", "thermal_status"} {
		if _, ok := l.LastMetrics[key].(float64); !ok {
			return profile, ""
		}
	}
	core, err := a.config()
	if err != nil {
		return profile, ""
	}
	if _, explicit := core.Apps[pkg]; explicit {
		return profile, ""
	}
	location, _ := time.LoadLocation(c.Timezone)
	hour := at.In(location).Hour()
	var seconds, sessions, hourSeconds int64
	days := 0
	game := false
	for _, day := range l.History.Days {
		if day.Timezone != c.Timezone {
			continue
		}
		if entry := day.Apps[pkg]; entry != nil && entry.Seconds > 0 {
			days++
			seconds += entry.Seconds
			sessions += entry.Sessions
			hourSeconds += entry.Hours[hour]
			game = game || entry.GameSupported
		}
	}
	if days >= 2 && seconds >= 1800 && sessions >= 3 && hourSeconds >= 600 && game {
		return "native-game", "adaptive-learned-game"
	}
	return profile, ""
}

type HabitApp struct {
	Package       string `json:"package"`
	ID            string `json:"id"`
	Seconds       int64  `json:"seconds"`
	Sessions      int64  `json:"sessions"`
	Days          int    `json:"days"`
	GameSupported bool   `json:"game_supported"`
	Profile       string `json:"profile"`
}

func historyApps(h UsageHistory, c Config) []HabitApp {
	items := map[string]*HabitApp{}
	for _, day := range h.Days {
		for pkg, entry := range day.Apps {
			if items[pkg] == nil {
				items[pkg] = &HabitApp{Package: pkg, Profile: c.Apps[pkg]}
			}
			x := items[pkg]
			x.Seconds += entry.Seconds
			x.Sessions += entry.Sessions
			if entry.Seconds > 0 {
				x.Days++
			}
			x.GameSupported = x.GameSupported || entry.GameSupported
		}
	}
	list := []HabitApp{}
	for _, entry := range items {
		list = append(list, *entry)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Seconds == list[j].Seconds {
			return list[i].Package < list[j].Package
		}
		return list[i].Seconds > list[j].Seconds
	})
	if len(list) > 16 {
		list = list[:16]
	}
	for i := range list {
		list[i].ID = fmt.Sprintf("app-%02d", i+1)
	}
	return list
}
func (a *App) LearningStatus(ctx context.Context) map[string]any {
	c, err := a.learningConfig()
	if err != nil {
		return map[string]any{"error": "learning-config-invalid"}
	}
	h, err := a.usageHistory()
	if err != nil {
		return map[string]any{"config": c, "error": "learning-history-invalid"}
	}
	core, _ := a.config()
	l := Learner{History: h, Config: c}
	l.day(time.Now())
	h = l.History
	var daily DailyResult
	_ = readAIJSON(a.state("daily-ai.json"), &daily)
	var attempt DailyAttempt
	_ = readJSON(a.state("daily-attempt.json"), &attempt)
	return map[string]any{"config": c, "history": h, "apps": historyApps(h, core), "daily": daily, "last_attempt": attempt, "retention_days": 7, "sampling_seconds": 60, "flush_seconds": 300}
}
