package experience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *App) restoreEntries(ctx context.Context, j *Journal, match func(Entry) bool) error {
	remaining := []Entry{}
	failed := false
	matched := false
	for i := len(j.Entries) - 1; i >= 0; i-- {
		e := j.Entries[i]
		if !match(e) {
			remaining = append([]Entry{e}, remaining...)
			continue
		}
		matched = true
		current, err := a.current(ctx, e)
		if err != nil {
			remaining = append([]Entry{e}, remaining...)
			failed = true
			continue
		}
		if current == e.Before {
			continue
		} // Crash before the original write.
		if current != e.Applied {
			e.Conflict = true
			remaining = append([]Entry{e}, remaining...)
			failed = true
			continue
		}
		if a.writeValue(ctx, e, e.Before) != nil {
			remaining = append([]Entry{e}, remaining...)
			failed = true
			continue
		}
		if v, err := a.current(ctx, e); err != nil || v != e.Before {
			remaining = append([]Entry{e}, remaining...)
			failed = true
		}
	}
	j.Entries = remaining
	if matched {
		if err := atomicJSON(a.state("journal.json"), j); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("restore-conflict: some settings need review; external changes were preserved")
	}
	return nil
}

func (a *App) apply(ctx context.Context, j *Journal, e Entry) error {
	for i, old := range j.Entries {
		if old.Kind == e.Kind && old.Package == e.Package {
			if old.Conflict {
				return errors.New("restore-conflict")
			}
			current, err := a.current(ctx, old)
			if err != nil {
				return err
			}
			if current == old.Applied {
				return nil
			}
			if current != old.Before {
				j.Entries[i].Conflict = true
				_ = atomicJSON(a.state("journal.json"), j)
				return errors.New("restore-conflict")
			}
			j.Entries = append(j.Entries[:i], j.Entries[i+1:]...)
			if err = atomicJSON(a.state("journal.json"), j); err != nil {
				return err
			}
			break
		}
	}
	before, err := a.current(ctx, e)
	if err != nil {
		return err
	}
	if before == e.Applied {
		return nil
	}
	e.Before = before
	j.Boot = a.bootID()
	j.Entries = append(j.Entries, e)
	if err := atomicJSON(a.state("journal.json"), j); err != nil {
		j.Entries = j.Entries[:len(j.Entries)-1]
		return err
	}
	if err := a.writeValue(ctx, e, e.Applied); err != nil {
		return err
	}
	got, err := a.current(ctx, e)
	if err != nil || got != e.Applied {
		return errors.New("setting verification failed")
	}
	return nil
}

func (a *App) restoreAll(ctx context.Context) error {
	j, e := a.journal()
	if e != nil {
		return e
	}
	return a.restoreEntries(ctx, &j, func(Entry) bool { return true })
}

func (a *App) moduleDisabled() bool {
	return !exists(a.Paths.Module) || exists(filepath.Join(a.Paths.Module, "disable")) || exists(filepath.Join(a.Paths.Module, "remove"))
}

// Recover is also called by the independent, one-shot service.d cleanup hook.
func (a *App) Recover(ctx context.Context) error {
	unlock, e := a.lock("state")
	if e != nil {
		return e
	}
	defer unlock()
	if err := a.recoverSettingsTransaction(ctx); err != nil {
		return err
	}
	j, e := a.journal()
	if e != nil {
		return e
	}
	if a.moduleDisabled() || j.Boot != a.bootID() {
		if err := a.restoreEntries(ctx, &j, func(Entry) bool { return true }); err != nil {
			return err
		}
		if a.moduleDisabled() {
			c, e := a.config()
			if e != nil {
				data, _ := os.ReadFile(a.state("config.json"))
				if e = atomicWrite(a.state("config.invalid.backup"), data, 0600); e != nil {
					return e
				}
				c = disabledConfig()
			}
			c.Enabled = false
			c.DefaultsVersion = 1
			if e = atomicJSON(a.state("config.json"), c); e != nil {
				return e
			}
		}
	}
	return nil
}

func (a *App) migrateLegacy(ctx context.Context, c *Config) error {
	if err := a.restoreAll(ctx); err != nil {
		return err
	}
	c.Generation++
	if exists(a.Paths.Legacy) {
		data, e := os.ReadFile(a.Paths.Legacy)
		if e != nil {
			return e
		}
		c.LegacySHA256 = digest(data)
		backup := a.state("legacy-" + c.LegacySHA256 + ".sh")
		if e = atomicWrite(backup, data, 0600); e != nil {
			return e
		}
		// Preserve source before removing its executable service.d name.
		if e = os.Remove(a.Paths.Legacy); e != nil {
			return e
		}
		c.MigrationBoot = a.bootID()
	}
	return atomicJSON(a.state("config.json"), c)
}

func (a *App) Install(ctx context.Context, executable string) error {
	if e := a.compatible(ctx); e != nil {
		return e
	}
	unlock, e := a.lock("state")
	if e != nil {
		return e
	}
	defer unlock()
	c, e := a.config()
	if e != nil {
		return e
	}
	// Adopt the new daily default once for the untouched, empty old setup.
	// Configured app rules and subsequent explicit disable choices are preserved.
	if c.DefaultsVersion < 1 {
		if len(c.Apps) == 0 && len(c.Protected) == 0 {
			c.Enabled = true
		}
		c.DefaultsVersion = 1
	}
	if e = a.migrateLegacy(ctx, &c); e != nil {
		return e
	}
	if e = a.initLearning(ctx); e != nil {
		return e
	}
	if e = a.upgradeAI(); e != nil {
		return e
	}
	// A standalone binary allows cleanup even if the Manager skips a disabled module.
	data, e := os.ReadFile(executable)
	if e != nil {
		return e
	}
	if e = atomicWrite(a.state("cleanup"), data, 0700); e != nil {
		return e
	}
	hook := "#!/system/bin/sh\n/data/adb/houji-experience/cleanup cleanup-boot >/dev/null 2>&1\n"
	if e = atomicWrite(a.Paths.Hook, []byte(hook), 0755); e != nil {
		return e
	}
	a.logEvent("installed-preview")
	return nil
}

func (a *App) Uninstall(ctx context.Context) error {
	defer a.clearLearning()
	unlock, e := a.lock("state")
	if e != nil {
		return e
	}
	defer unlock()
	// Credentials must be removed even when configuration/restore is broken.
	defer os.Remove(a.state("ai.json"))
	c, e := a.config()
	if e != nil {
		data, _ := os.ReadFile(a.state("config.json"))
		if e = atomicWrite(a.state("config.invalid.backup"), data, 0600); e != nil {
			return e
		}
		c = disabledConfig()
	}
	c.Enabled = false
	if e = atomicJSON(a.state("config.json"), c); e != nil {
		return e
	}
	err := a.restoreAll(ctx)
	_ = os.Remove(a.state("ai.json"))
	if err != nil {
		a.logEvent("uninstall-restore-pending")
		return err
	}
	_ = os.Remove(a.Paths.Hook)
	a.logEvent("uninstalled-backups-retained")
	return nil
}

type Runtime struct {
	Profile string `json:"profile"`
	Package string `json:"package,omitempty"`
	Reason  string `json:"reason"`
	Updated string `json:"updated"`
}

func (a *App) runtime(profile, pkg, reason string) {
	old := Runtime{}
	_ = readJSON(a.state("runtime.json"), &old)
	if old.Profile == profile && old.Package == pkg && old.Reason == reason {
		return
	}
	r := Runtime{Profile: profile, Package: pkg, Reason: reason, Updated: time.Now().UTC().Format(time.RFC3339)}
	_ = atomicJSON(a.state("runtime.json"), r)
	a.logEvent("profile-" + profile)
}

func (a *App) protect(ctx context.Context, c Config, j *Journal) error {
	// Remove only owned exceptions no longer selected. Never clear global lists.
	if err := a.restoreEntries(ctx, j, func(e Entry) bool { return e.Kind == "doze" && !c.Protected[e.Package] }); err != nil {
		return err
	}
	keys := []string{}
	for p, enabled := range c.Protected {
		if enabled {
			keys = append(keys, p)
		}
	}
	sort.Strings(keys)
	for _, p := range keys {
		if !a.installed(ctx, p) {
			continue
		}
		if err := a.apply(ctx, j, Entry{Kind: "doze", Package: p, Applied: "true"}); err != nil {
			return err
		}
	}
	// MILLET is optional: an absent or unreadable OEM setting is not synthesized.
	var existing *Entry
	for i := range j.Entries {
		if j.Entries[i].Kind == "millet" {
			existing = &j.Entries[i]
			break
		}
	}
	base := ""
	if existing != nil {
		base = existing.Before
	} else {
		v, e := a.current(ctx, Entry{Kind: "millet"})
		if e != nil || v == "null" || v == "" {
			return nil
		}
		base = v
	}
	parts := strings.Split(base, ",")
	set := map[string]bool{}
	for _, p := range parts {
		set[strings.TrimSpace(p)] = true
	}
	for _, p := range keys {
		if !set[p] && a.installed(ctx, p) {
			parts = append(parts, p)
			set[p] = true
		}
	}
	wanted := strings.Join(parts, ",")
	if existing != nil && wanted == existing.Applied {
		return nil
	}
	if existing != nil {
		if err := a.restoreEntries(ctx, j, func(e Entry) bool { return e.Kind == "millet" }); err != nil {
			return err
		}
	}
	if wanted == base {
		return nil
	}
	return a.apply(ctx, j, Entry{Kind: "millet", Applied: wanted})
}

// Step mutates Android settings only while holding the same lock as WebUI/CLI.
func (a *App) Step(ctx context.Context, pkg string, asleep bool) error {
	unlock, e := a.lock("state")
	if e != nil {
		return e
	}
	defer unlock()
	if err := a.recoverSettingsTransaction(ctx); err != nil {
		return err
	}
	c, e := a.config()
	if e != nil {
		_ = a.restoreAll(ctx)
		a.runtime("observe", "", "configuration-invalid")
		return e
	}
	j, e := a.journal()
	if e != nil {
		return e
	}
	reason := ""
	if !c.Enabled || a.moduleDisabled() {
		reason = "disabled"
	} else if exists(a.Paths.Legacy) {
		reason = "legacy-conflict"
	} else if c.MigrationBoot != "" && c.MigrationBoot == a.bootID() {
		reason = "reboot-required"
	} else if !a.ownerUser(ctx) {
		reason = "owner-user-only"
	} else if sync := a.KernelSync(ctx); !sync.Ready {
		reason = "kernel-" + sync.State
	}
	if reason != "" {
		a.protectedStamp = ""
		a.writeFailureStamp = ""
		a.writeFailureReason = ""
		err := a.restoreEntries(ctx, &j, func(Entry) bool { return true })
		a.runtime("observe", "", reason)
		return err
	}
	kernelStamp := fmt.Sprintf("%d:%s", c.Generation, a.bootID())
	if a.writeFailureStamp == kernelStamp {
		a.runtime("observe", "", a.writeFailureReason)
		return nil
	}
	if err := a.tuneKernel(ctx, c, &j); err != nil {
		restoreErr := a.restoreEntries(ctx, &j, func(Entry) bool { return true })
		a.protectedStamp = ""
		if restoreErr == nil && (err.Error() == "proposal-pressure" || err.Error() == "proposal-pressure-unknown") {
			reason := "kernel-pressure"
			if err.Error() == "proposal-pressure-unknown" {
				reason = "kernel-pressure-unknown"
			}
			a.runtime("observe", "", reason)
			return nil
		}
		a.writeFailureStamp = kernelStamp
		a.writeFailureReason = "kernel-setting-failed"
		a.runtime("observe", "", "kernel-setting-failed")
		return err
	}
	stampBytes, _ := json.Marshal(c.Protected)
	stamp := fmt.Sprintf("%d:%s", c.Generation, digest(stampBytes))
	if a.protectedStamp != stamp {
		if err := a.tuneSystem(ctx, c, &j); err != nil {
			_ = a.restoreEntries(ctx, &j, func(Entry) bool { return true })
			a.writeFailureStamp = kernelStamp
			a.writeFailureReason = "setting-failed"
			a.runtime("observe", "", "setting-failed")
			return err
		}
		if err := a.protect(ctx, c, &j); err != nil {
			_ = a.restoreEntries(ctx, &j, func(Entry) bool { return true })
			a.writeFailureStamp = kernelStamp
			a.writeFailureReason = "restore-conflict"
			a.runtime("observe", "", "restore-conflict")
			return err
		}
		a.protectedStamp = stamp
	}
	profile := "daily"
	if p, ok := c.Apps[pkg]; ok && profiles[p] {
		profile = p
	}
	if asleep {
		profile = "sleep"
		pkg = ""
	}
	profile, adaptiveReason := a.adaptiveProfile(ctx, profile, pkg, asleep, time.Now())
	if err := a.restoreEntries(ctx, &j, func(e Entry) bool { return e.Kind == "game" && (profile != "native-game" || e.Package != pkg) }); err != nil {
		a.runtime("observe", "", "restore-conflict")
		return err
	}
	if profile == "native-game" && validPackage(pkg) {
		o, err := a.run(ctx, "cmd", "game", "list-modes", pkg)
		if err != nil || !strings.Contains(o, "available game modes: [") || !strings.Contains(strings.Split(o, "available game modes: [")[1], "performance") {
			a.runtime(profile, pkg, "game-mode-unsupported")
			return nil
		}
		if err = a.apply(ctx, &j, Entry{Kind: "game", Package: pkg, Applied: "performance"}); err != nil {
			_ = a.restoreEntries(ctx, &j, func(e Entry) bool { return e.Kind == "game" })
			a.runtime("observe", "", "setting-failed")
			return err
		}
	}
	reason = "rules-local"
	if profile == "daily" {
		reason = "daily-default"
	}
	if profile == "windows-game" || profile == "linux" {
		reason = "integration-observe"
	}
	if adaptiveReason != "" {
		reason = adaptiveReason
	}
	a.runtime(profile, pkg, reason)
	return nil
}

func (a *App) Watch(ctx context.Context) error {
	if err := a.migrateConfiguration(); err != nil {
		return err
	}
	if e := a.compatible(ctx); e != nil {
		return e
	}
	unlock, e := a.lock("watcher")
	if e != nil {
		return e
	}
	defer unlock()
	if e = a.Recover(ctx); e != nil {
		a.runtime("observe", "", "restore-conflict")
	}
	if c, err := a.config(); err == nil && c.BootNotify && a.ownerUser(ctx) {
		_, _ = a.run(ctx, "cmd", "notification", "post", "-S", "bigtext", "-t", "HNHouji Experience", "houji_boot", "Watcher đã khởi động. Xem WebUI để kiểm tra trạng thái áp dụng.")
	}
	previous := ""
	count := 0
	lastErrorAt := time.Time{}
	for {
		pollCtx, pollCancel := context.WithTimeout(ctx, 2*time.Second)
		pkg, asleep := "", false
		config, configErr := a.config()
		if configErr == nil && config.Enabled && !a.moduleDisabled() && !exists(a.Paths.Legacy) {
			pkg, asleep = a.foreground(pollCtx)
		}
		pollCancel()
		candidate := fmt.Sprintf("%t:%s", asleep, pkg)
		if candidate == previous {
			count++
		} else {
			previous = candidate
			count = 1
		}
		if count >= 2 || asleep || pkg == "" {
			_ = a.Observe(ctx, pkg, asleep, time.Now())
			if e = a.Step(ctx, pkg, asleep); e != nil {
				if time.Since(lastErrorAt) > 5*time.Minute {
					a.logEvent("step-needs-review")
					lastErrorAt = time.Now()
				}
			}
			a.maybeDailyAI(ctx, time.Now())
		}
		wait := 3 * time.Second
		if asleep {
			wait = 20 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			if a.learner != nil {
				_ = a.flushLearning(a.learner, time.Now())
			}
			u, err := a.lock("state")
			if err == nil {
				_ = a.restoreAll(context.Background())
				u()
			}
			return nil
		case <-timer.C:
		}
	}
}

func (a *App) snapshotConfig() map[string]any {
	c, e := a.config()
	if e != nil {
		return map[string]any{"error": "configuration-invalid"}
	}
	return map[string]any{"enabled": c.Enabled, "kernel_tuning": c.KernelTuning, "kernel_controls": c.KernelControls, "kernel_levels": c.KernelLevels, "system_settings": c.SystemSettings, "boot_notify": c.BootNotify, "default_profile": "daily", "apps": c.Apps, "protected": c.Protected, "reboot_required": c.MigrationBoot != "" && c.MigrationBoot == a.bootID()}
}

func (a *App) CleanupBoot(ctx context.Context) error {
	if !exists(a.Paths.Module) {
		a.clearLearning()
		_ = os.Remove(a.state("ai.json"))
	}
	if e := a.Recover(ctx); e != nil {
		return e
	}
	if !exists(a.Paths.Module) {
		_ = os.Remove(a.state("ai.json"))
		_ = os.Remove(a.Paths.Hook)
		_ = os.Remove(a.state("cleanup"))
	}
	return nil
}

func marshalEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
