package experience

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"time"
)

func cloneConfig(c Config) Config {
	b, _ := json.Marshal(c)
	var result Config
	_ = json.Unmarshal(b, &result)
	return result
}
func (a *App) manualSettingsReady(ctx context.Context, c Config) error {
	if !a.ownerUser(ctx) {
		return errors.New("owner-user-only")
	}
	if !c.Enabled || a.moduleDisabled() {
		return errors.New("module disabled")
	}
	if exists(a.Paths.Legacy) || (c.MigrationBoot != "" && c.MigrationBoot == a.bootID()) {
		return errors.New("reboot-required")
	}
	if err := a.compatible(ctx); err != nil {
		return err
	}
	if !a.KernelSync(ctx).Ready {
		return errors.New("proposal-kernel-mismatch")
	}
	return nil
}
func (a *App) pressureReady(ctx context.Context) error {
	var metrics map[string]any
	if a.learner != nil && !a.learner.SampleAt.IsZero() && time.Since(a.learner.SampleAt) >= 0 && time.Since(a.learner.SampleAt) < 2*time.Minute {
		metrics = a.learner.LastMetrics
	} else {
		metrics = a.Report(ctx)["metrics"].(map[string]any)
	}
	full, fok := metrics["psi_full_avg10"].(float64)
	thermal, tok := metrics["thermal_status"].(float64)
	if !fok || !tok || math.IsNaN(full) || math.IsInf(full, 0) || math.IsNaN(thermal) || math.IsInf(thermal, 0) || full < 0 || thermal < 0 {
		return errors.New("proposal-pressure-unknown")
	}
	if full >= 2 || thermal >= 3 {
		return errors.New("proposal-pressure")
	}
	return nil
}
func (a *App) levelAvailable(c Config, id, level string) bool {
	if level == "stock" {
		return validKernelControl(id)
	}
	for _, knob := range a.configuredKernelPlan(c) {
		if knob.Control == id && knob.Current != "" && hasLevel(knob, level) {
			return true
		}
	}
	return false
}
func (a *App) restoreSettingsDelta(ctx context.Context, before, after Config) error {
	j, err := a.journal()
	if err != nil {
		return err
	}
	return a.restoreEntries(ctx, &j, func(e Entry) bool {
		if !after.Enabled {
			return true
		}
		if e.Kind == "kernel" {
			id := kernelControlForKnob(e.Package)
			return before.KernelTuning != after.KernelTuning || kernelControlEnabled(before, id) != kernelControlEnabled(after, id) || kernelLevel(before, id) != kernelLevel(after, id)
		}
		if e.Kind == "system" {
			id, _, _ := strings.Cut(e.Package, ":")
			return before.SystemSettings[id] != after.SystemSettings[id]
		}
		return false
	})
}
func (a *App) saveSettingsConfig(ctx context.Context, before, after Config) error {
	if err := a.restoreSettingsDelta(ctx, before, after); err != nil {
		return err
	}
	after.Generation = before.Generation + 1
	return atomicJSON(a.state("config.json"), after)
}

// This portable contract deliberately cannot carry credentials, journal or receipts.
type UserSettings struct {
	Enabled        bool              `json:"enabled"`
	KernelTuning   bool              `json:"kernel_tuning"`
	KernelControls map[string]bool   `json:"kernel_controls"`
	KernelLevels   map[string]string `json:"kernel_levels"`
	SystemSettings map[string]string `json:"system_settings"`
	BootNotify     bool              `json:"boot_notify"`
	Apps           map[string]string `json:"apps"`
	Protected      map[string]bool   `json:"protected"`
}
type SettingsBackup struct {
	Version  int            `json:"version"`
	Settings UserSettings   `json:"settings"`
	Learning LearningConfig `json:"learning"`
}

func userSettings(c Config) UserSettings {
	return UserSettings{c.Enabled, c.KernelTuning, c.KernelControls, c.KernelLevels, c.SystemSettings, c.BootNotify, c.Apps, c.Protected}
}
func (a *App) settingsBackup() (SettingsBackup, error) {
	if !a.ownerUser(context.Background()) {
		return SettingsBackup{}, errors.New("owner-user-only")
	}
	u, err := a.lock("state")
	if err != nil {
		return SettingsBackup{}, err
	}
	defer u()
	c, err := a.config()
	if err != nil {
		return SettingsBackup{}, err
	}
	l, err := a.learningConfig()
	l.Generation = 0
	return SettingsBackup{1, userSettings(c), l}, err
}

type SettingsPreview struct {
	Token          string         `json:"token"`
	Created        time.Time      `json:"created"`
	Boot           string         `json:"boot"`
	BaseHash       string         `json:"base_hash"`
	LearningHash   string         `json:"learning_hash"`
	Before         Config         `json:"before"`
	After          Config         `json:"after"`
	BeforeLearning LearningConfig `json:"before_learning"`
	AfterLearning  LearningConfig `json:"after_learning"`
	Skipped        []string       `json:"skipped"`
}

func (a *App) previewSettings(ctx context.Context, r Request) (map[string]any, error) {
	if !a.ownerUser(ctx) {
		return nil, errors.New("owner-user-only")
	}
	u, err := a.lock("state")
	if err != nil {
		return nil, err
	}
	defer u()
	c, err := a.config()
	if err != nil {
		return nil, err
	}
	l, err := a.learningConfig()
	if err != nil {
		return nil, err
	}
	b := SettingsBackup{1, userSettings(cloneConfig(c)), l}
	switch r.Mode {
	case "stock":
		b.Settings.KernelTuning = false
		b.Settings.KernelLevels = map[string]string{"swap_readahead": "stock", "tcp_congestion": "stock", "data_fsync": "stock"}
		b.Settings.SystemSettings = map[string]string{}
	case "kernel":
		b.Settings.KernelTuning = true
		b.Settings.KernelControls = map[string]bool{}
		b.Settings.KernelLevels = map[string]string{"swap_readahead": "0", "tcp_congestion": "bbr", "data_fsync": "20"}
	case "custom":
		if err = readJSON(a.state("settings-custom.json"), &b); err != nil {
			return nil, err
		}
	case "save_custom":
		if err = atomicJSON(a.state("settings-custom.json"), b); err != nil {
			return nil, err
		}
		return map[string]any{"saved": true}, nil
	case "import":
		if r.Backup == nil {
			return nil, errors.New("missing backup")
		}
		b = *r.Backup
	default:
		return nil, errors.New("unknown settings preset")
	}
	if b.Version != 1 || len(b.Settings.Apps) > 100 || len(b.Settings.Protected) > 100 {
		return nil, errors.New("invalid settings backup")
	}
	next := cloneConfig(c)
	s := b.Settings
	next.Enabled = s.Enabled
	next.KernelTuning = s.KernelTuning
	next.KernelControls = s.KernelControls
	next.KernelLevels = s.KernelLevels
	next.SystemSettings = s.SystemSettings
	next.BootNotify = s.BootNotify
	next.Apps = s.Apps
	next.Protected = s.Protected
	for id := range next.KernelControls {
		if !validKernelControl(id) {
			return nil, errors.New("unknown kernel control")
		}
	}
	for id, level := range next.KernelLevels {
		if !validKernelLevel(id, level) {
			return nil, errors.New("unknown kernel level")
		}
	}
	for id, choice := range next.SystemSettings {
		if !validSystemChoice(id, choice) {
			return nil, errors.New("unknown system choice")
		}
	}
	if next.Apps == nil {
		next.Apps = map[string]string{}
	}
	if next.Protected == nil {
		next.Protected = map[string]bool{}
	}
	skipped := []string{}
	for pkg, profile := range next.Apps {
		if !validPackage(pkg) || !profiles[profile] {
			return nil, errors.New("invalid app rule")
		}
		if !a.installed(ctx, pkg) {
			delete(next.Apps, pkg)
			skipped = append(skipped, pkg)
		}
	}
	for pkg := range next.Protected {
		if !validPackage(pkg) {
			return nil, errors.New("invalid protected app")
		}
		if !a.installed(ctx, pkg) {
			delete(next.Protected, pkg)
			skipped = append(skipped, pkg)
		}
	}
	// Importing a file never grants networking or scheduled application consent.
	b.Learning.DailyAI = l.DailyAI
	b.Learning.AutoApply = l.AutoApply
	b.Learning.Generation = l.Generation + 1
	if err = b.Learning.validate(); err != nil {
		return nil, err
	}
	if err = a.validateSettingsTarget(ctx, next); err != nil {
		return nil, err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	next.Generation = c.Generation + 1
	if r.Mode == "import" || r.Mode == "custom" {
		next.AIProfiles = map[string]string{}
	}
	p := SettingsPreview{hex.EncodeToString(random[:]), time.Now().UTC(), a.bootID(), configHash(c), learningHash(l), c, next, l, b.Learning, skipped}
	if err = atomicJSON(a.state("settings-preview.json"), p); err != nil {
		return nil, err
	}
	return map[string]any{"token": p.Token, "before": userSettings(c), "after": userSettings(next), "learning": p.AfterLearning, "skipped": skipped, "note": "Quyền gọi AI và tự áp dụng hiện tại được giữ nguyên."}, nil
}
func learningHash(c LearningConfig) string { b, _ := json.Marshal(c); return digest(b) }
func (a *App) validateSettingsTarget(ctx context.Context, c Config) error {
	writes := false
	if c.KernelTuning {
		for _, id := range []string{"swap_readahead", "tcp_congestion", "data_fsync"} {
			if kernelControlEnabled(c, id) && kernelLevel(c, id) != "stock" {
				writes = true
				if !a.levelAvailable(c, id, kernelLevel(c, id)) {
					return errors.New("proposal-kernel-unsupported")
				}
			}
		}
	}
	for _, choice := range c.SystemSettings {
		writes = writes || choice != "stock"
	}
	if writes && c.Enabled {
		if err := a.manualSettingsReady(ctx, c); err != nil {
			return err
		}
		if err := a.pressureReady(ctx); err != nil {
			return err
		}
	}
	return a.validateSystemChoices(ctx, c)
}
func (a *App) applySettingsPreview(ctx context.Context, token string) (map[string]any, error) {
	if !a.ownerUser(ctx) {
		return nil, errors.New("owner-user-only")
	}
	u, err := a.lock("state")
	if err != nil {
		return nil, err
	}
	defer u()
	v, err := a.lock("learning")
	if err != nil {
		return nil, err
	}
	defer v()
	var p SettingsPreview
	if readJSON(a.state("settings-preview.json"), &p) != nil || token == "" || p.Token != token || p.Boot != a.bootID() || time.Since(p.Created) < 0 || time.Since(p.Created) > 10*time.Minute {
		return nil, errors.New("settings-preview-expired")
	}
	c, err := a.config()
	if err != nil {
		return nil, err
	}
	l, err := a.learningConfig()
	if err != nil {
		return nil, err
	}
	if configHash(c) != p.BaseHash || learningHash(l) != p.LearningHash {
		return nil, errors.New("settings-preview-stale")
	}
	if err = a.validateSettingsTarget(ctx, p.After); err != nil {
		return nil, err
	}
	if err = atomicJSON(a.state("settings-transaction.json"), p); err != nil {
		return nil, err
	}
	if err = a.completeSettingsTransaction(ctx); err != nil {
		return nil, err
	}
	_ = os.Remove(a.state("settings-preview.json"))
	return a.snapshotConfig(), nil
}
func (a *App) recoverSettingsTransaction(ctx context.Context) error {
	if !exists(a.state("settings-transaction.json")) {
		return nil
	}
	u, err := a.lock("learning")
	if err != nil {
		return err
	}
	defer u()
	return a.completeSettingsTransaction(ctx)
}
func (a *App) completeSettingsTransaction(ctx context.Context) error {
	var p SettingsPreview
	err := readJSON(a.state("settings-transaction.json"), &p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	l, err := a.learningConfig()
	if err != nil {
		return err
	}
	if (configHash(c) != configHash(p.Before) && configHash(c) != configHash(p.After)) || (learningHash(l) != learningHash(p.BeforeLearning) && learningHash(l) != learningHash(p.AfterLearning)) {
		return errors.New("settings-transaction-conflict")
	}
	if err = a.restoreSettingsDelta(ctx, p.Before, p.After); err != nil {
		return err
	}
	if err = atomicJSON(a.state("config.json"), p.After); err != nil {
		return err
	}
	if err = atomicJSON(a.state("learning-config.json"), p.AfterLearning); err != nil {
		return err
	}
	return os.Remove(a.state("settings-transaction.json"))
}
