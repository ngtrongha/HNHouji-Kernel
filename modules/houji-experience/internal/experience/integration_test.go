package experience

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKernelLevelsSwitchAndRestoreOriginal(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	for _, level := range []string{"0", "1", "2", "3", "stock", "1", "stock"} {
		r := a.Dispatch(ctx, Request{Action: "kernel_level_set", Control: "swap_readahead", Level: level})
		if !r.OK {
			t.Fatal(r)
		}
		must(t, a.Step(ctx, "", false))
		want := level
		if level == "stock" {
			want = "3"
		}
		knobValue(t, a, "vm.page-cluster", want)
	}
	if r := a.Dispatch(ctx, Request{Action: "kernel_level_set", Control: "swap_readahead", Level: "4"}); r.OK {
		t.Fatal("unbounded level")
	}
	must(t, a.writeKernelKnob("vm.page-cluster", "2"))
	r := a.Dispatch(ctx, Request{Action: "kernel_level_set", Control: "swap_readahead", Level: "0"})
	if !r.OK {
		t.Fatal(r)
	}
	must(t, a.Step(ctx, "", false))
	must(t, a.writeKernelKnob("vm.page-cluster", "1"))
	if r := a.Dispatch(ctx, Request{Action: "kernel_level_set", Control: "swap_readahead", Level: "stock"}); r.OK {
		t.Fatal("external override overwritten")
	}
	knobValue(t, a, "vm.page-cluster", "1")
}
func TestLevelProposalApplyUndoAndMixedActions(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	c, _ := a.config()
	l, _ := a.learningConfig()
	p, err := makeProposal([]AIAction{{Kind: "kernel_level", Control: "swap_readahead", Level: "1", Reason: "Đối chiếu đọc đón"}}, nil, c, l.Generation, a.bootID(), time.Now())
	must(t, err)
	must(t, atomicJSON(a.state("ai-proposal.json"), p))
	_, err = a.applyProposal(ctx, p.ID, false)
	must(t, err)
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "1")
	_, err = a.undoAI(ctx)
	must(t, err)
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "0")
	on := true
	for _, actions := range [][]AIAction{
		{{Kind: "kernel_level", Control: "swap_readahead", Level: "7", Reason: "x"}},
		{{Kind: "kernel_level", Control: "swap_readahead", Level: "1", Enabled: &on, Reason: "x"}},
		{{Kind: "kernel_control", Control: "swap_readahead", Enabled: &on, Reason: "x"}, {Kind: "kernel_level", Control: "swap_readahead", Level: "1", Reason: "x"}},
		{{Kind: "system_setting", Control: "animation", Level: "0.5", Reason: "x"}},
	} {
		if _, err = makeProposal(actions, nil, c, l.Generation, a.bootID(), time.Now()); err == nil {
			t.Fatal("invalid action accepted", actions)
		}
	}
}
func TestMigrationPreservesLevelsDisableAndRejectsUnknown(t *testing.T) {
	a, _ := fixture(t)
	must(t, atomicWrite(a.state("config.json"), []byte(`{"version":1,"generation":4,"enabled":false,"kernel_tuning":false,"kernel_controls":{"tcp_congestion":false}}`), 0600))
	must(t, a.migrateConfiguration())
	c, err := a.config()
	must(t, err)
	if c.Version != 2 || c.Generation != 5 || c.Enabled || c.KernelTuning || kernelControlEnabled(c, "tcp_congestion") || kernelLevel(c, "data_fsync") != "20" {
		t.Fatal(c)
	}
	must(t, a.migrateConfiguration())
	again, _ := a.config()
	if configHash(c) != configHash(again) {
		t.Fatal("repeated migration")
	}
	c.KernelLevels = map[string]string{"thermal": "0"}
	must(t, atomicJSON(a.state("config.json"), c))
	if _, err = a.config(); err == nil {
		t.Fatal("unknown config accepted")
	}
}

type settingsAndroid struct {
	base    *fakeAndroid
	values  map[string]string
	display string
	fail    string
}

func (f *settingsAndroid) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "settings" && len(args) == 4 && args[2] == "list" {
		lines := []string{}
		for key, value := range f.values {
			namespace, key, _ := strings.Cut(key, ":")
			if namespace == args[3] {
				lines = append(lines, key+"="+value)
			}
		}
		return strings.Join(lines, "\n"), nil
	}
	if name == "dumpsys" && len(args) > 0 && args[0] == "display" {
		return f.display, nil
	}
	if name == "settings" && len(args) >= 5 && args[4] != "MILLET_NO_RESTRICT_APP" {
		key := args[3] + ":" + args[4]
		if args[2] == "get" {
			if v, ok := f.values[key]; ok {
				return v, nil
			}
			return "null", nil
		}
		if key == f.fail {
			return "", errors.New("interrupted")
		}
		f.base.writes++
		if args[2] == "delete" {
			delete(f.values, key)
		} else {
			f.values[key] = args[5]
		}
		return "", nil
	}
	return f.base.Run(ctx, name, args...)
}
func settingsFixture(t *testing.T) (*App, *settingsAndroid) {
	a, base := fixture(t)
	seedKnobs(t, a)
	f := &settingsAndroid{base: base, values: map[string]string{}, display: "mBaseDisplayInfo=DisplayInfo{displayId 0, supportedModes=[{fps=60.0},{fps=120.0}]}\nmBaseDisplayInfo=DisplayInfo{displayId 1, supportedModes=[{fps=90.0}]}"}
	for _, d := range systemDefinitions {
		for _, target := range d.Targets(d.Choices[1]) {
			f.values[target.Namespace+":"+target.Key] = "1"
		}
	}
	f.values["global:battery_saver_constants"] = "other=keep,adjust_brightness_factor=0.95"
	f.values["system:min_refresh_rate"] = "0"
	a.Runner = f
	return a, f
}
func TestSystemChoicesCapabilityJournalAndFieldUndo(t *testing.T) {
	a, f := settingsFixture(t)
	ctx := context.Background()
	for _, choice := range []string{"80", "70", "stock"} {
		r := a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "saver_dim", Choice: choice})
		if !r.OK {
			t.Fatal(r)
		}
		must(t, a.Step(ctx, "", false))
	}
	if got := f.values["global:battery_saver_constants"]; !strings.Contains(got, "other=keep") || !strings.Contains(got, "adjust_brightness_factor=0.95") {
		t.Fatal(got)
	}
	r := a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "saver_dim", Choice: "80"})
	if !r.OK {
		t.Fatal(r)
	}
	must(t, a.Step(ctx, "", false))
	f.values["global:battery_saver_constants"] += " ,external=changed"
	f.values["global:battery_saver_constants"] = strings.ReplaceAll(f.values["global:battery_saver_constants"], " ,", ",")
	r = a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "saver_dim", Choice: "stock"})
	if !r.OK {
		t.Fatal(r)
	}
	if !strings.Contains(f.values["global:battery_saver_constants"], "external=changed") {
		t.Fatal("unrelated field lost")
	}
	f.values["system:min_refresh_rate"] = "90"
	for _, choice := range []string{"60", "90"} {
		if r = a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "peak_refresh", Choice: choice}); r.OK {
			t.Fatal("unavailable refresh accepted", choice)
		}
	}
	if r = a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "peak_refresh", Choice: "120"}); !r.OK {
		t.Fatal(r)
	}
	delete(f.values, "global:wifi_scan_always_enabled")
	if r = a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "wifi_scanning", Choice: "off"}); r.OK {
		t.Fatal("missing key manufactured")
	}
}
func TestSystemExternalChangeAndPartialWriteAreRecoverable(t *testing.T) {
	a, f := settingsFixture(t)
	ctx := context.Background()
	r := a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "animation", Choice: "0.5"})
	if !r.OK {
		t.Fatal(r)
	}
	f.fail = "global:transition_animation_scale"
	if a.Step(ctx, "", false) == nil {
		t.Fatal("write failure ignored")
	}
	f.fail = ""
	must(t, a.restoreAll(ctx))
	if f.values["global:window_animation_scale"] != "1" {
		t.Fatal("partial write left behind")
	}
	a.writeFailureStamp = ""
	must(t, a.Step(ctx, "", false))
	f.values["global:window_animation_scale"] = "0.25"
	if r = a.Dispatch(ctx, Request{Action: "system_setting_set", ID: "animation", Choice: "stock"}); r.OK {
		t.Fatal("external override lost")
	}
	if f.values["global:window_animation_scale"] != "0.25" {
		t.Fatal("external value overwritten")
	}
}
func TestBackupPreviewPrivacyStaleAndPermissions(t *testing.T) {
	a, f := settingsFixture(t)
	ctx := context.Background()
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	b, err := a.settingsBackup()
	must(t, err)
	if strings.Contains(pretty(b), "test-secret") || strings.Contains(pretty(b), "base_url") {
		t.Fatal("credentials exported")
	}
	b.Learning.DailyAI = true
	b.Learning.AutoApply = true
	b.Settings.KernelLevels = map[string]string{"swap_readahead": "1"}
	preview, err := a.previewSettings(ctx, Request{Mode: "import", Backup: &b})
	must(t, err)
	_, err = a.applySettingsPreview(ctx, preview["token"].(string))
	must(t, err)
	l, _ := a.learningConfig()
	if l.DailyAI || l.AutoApply {
		t.Fatal("backup granted AI consent")
	}
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "1")
	preview, err = a.previewSettings(ctx, Request{Mode: "stock"})
	must(t, err)
	c, _ := a.config()
	c.Generation++
	must(t, atomicJSON(a.state("config.json"), c))
	if _, err = a.applySettingsPreview(ctx, preview["token"].(string)); err == nil {
		t.Fatal("stale preview accepted")
	}
	f.base.user = "10"
	if r := a.Dispatch(ctx, Request{Action: "kernel_level_set", Control: "swap_readahead", Level: "stock"}); r.OK {
		t.Fatal("secondary user changed settings")
	}
}
func TestSettingsTransactionRecoversConfigLearningAndKeepsReceipt(t *testing.T) {
	a, _ := settingsFixture(t)
	ctx := context.Background()
	preview, err := a.previewSettings(ctx, Request{Mode: "stock"})
	must(t, err)
	_ = preview
	var p SettingsPreview
	must(t, readJSON(a.state("settings-preview.json"), &p))
	must(t, atomicJSON(a.state("settings-transaction.json"), p))
	must(t, atomicJSON(a.state("config.json"), p.After))
	must(t, atomicJSON(a.state("ai-application.json"), AIApplication{Status: "applied", ProposalID: "keep"}))
	must(t, a.Recover(ctx))
	l, _ := a.learningConfig()
	if learningHash(l) != learningHash(p.AfterLearning) || exists(a.state("settings-transaction.json")) {
		t.Fatal("transaction not completed")
	}
	if a.ApplicationStatus().ProposalID != "keep" {
		t.Fatal("AI undo receipt overwritten")
	}
}

const checkinSample = "9,0,i,vers,11,116,K,L\n9,0,i,uid,10001,com.private.app\n9,0,l,bt,1,3600000,0,3600000,0,123456789,3600000,0\n9,0,l,dc,2,2,0,2\n"

type batteryAndroid struct {
	base  Runner
	raw   string
	err   error
	calls int
	level string
}

func (f *batteryAndroid) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "dumpsys" && len(args) > 0 && args[0] == "battery" && f.level != "" {
		return "level: " + f.level + "\ntemperature: 350", nil
	}
	if name == "dumpsys" && len(args) > 0 && args[0] == "batterystats" {
		f.calls++
		return f.raw, f.err
	}
	return f.base.Run(ctx, name, args...)
}
func TestBatteryParserWorkerBoundsPrivacyAndNoRetry(t *testing.T) {
	for _, raw := range []string{"", strings.ReplaceAll(checkinSample, "9,", "10,"), strings.Replace(checkinSample, ",3600000,0\n9,0,l,dc", ",-1,0\n9,0,l,dc", 1), strings.Repeat("x", maxBatteryBytes+1)} {
		if _, err := parseBatteryStats(raw); err == nil {
			t.Fatal("invalid statistics accepted")
		}
	}
	a, base := fixture(t)
	f := &batteryAndroid{base: base, raw: checkinSample}
	a.Runner = f
	must(t, atomicWrite(a.system("/sys/class/power_supply/battery/status"), []byte("Discharging"), 0600))
	j, err := a.startBatteryJob(true, func(string) error { return nil })
	must(t, err)
	must(t, a.RunBatteryJob(context.Background(), j["id"].(string)))
	status, err := a.batteryJobStatus(j["id"].(string))
	must(t, err)
	if status["state"] != "completed" || f.calls != 1 {
		t.Fatal(status, f.calls)
	}
	if rate := status["result"].(BatterySnapshot).Rate; rate == nil || *rate != 2 {
		t.Fatal(status)
	}
	files, err := os.ReadDir(a.Paths.State)
	must(t, err)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "battery") {
			b, _ := os.ReadFile(filepath.Join(a.Paths.State, file.Name()))
			if strings.Contains(string(b), "com.private") {
				t.Fatal("raw UID persisted")
			}
		}
	}
	must(t, atomicWrite(a.system("/sys/class/power_supply/battery/status"), []byte("Charging"), 0600))
	if a.batterySnapshotView().(BatterySnapshot).Rate != nil {
		t.Fatal("charging drain shown")
	}
	f.err = errors.New("private output must not be returned")
	j, err = a.startBatteryJob(true, func(string) error { return nil })
	must(t, err)
	must(t, a.RunBatteryJob(context.Background(), j["id"].(string)))
	status, err = a.batteryJobStatus("")
	must(t, err)
	if status["state"] != "failed" || f.calls != 2 || strings.Contains(pretty(status), "private output") {
		t.Fatal(status)
	}
}
func TestBatteryInterruptedJobsAndLaunchFailure(t *testing.T) {
	a, _ := fixture(t)
	job, err := a.startBatteryJob(true, func(string) error { return errors.New("no worker") })
	must(t, err)
	if job["state"] != "failed" {
		t.Fatal(job)
	}
	must(t, atomicJSON(a.state("battery-job.json"), BatteryJob{ID: "old", Boot: "old-boot", State: "running", Started: time.Now()}))
	status, err := a.batteryJobStatus("")
	must(t, err)
	if status["error"] != "battery-job-interrupted" {
		t.Fatal(status)
	}
}

func TestAllSupportedSystemChoicesRestoreAndRejectUnknown(t *testing.T) {
	for _, definition := range systemDefinitions {
		t.Run(definition.ID, func(t *testing.T) {
			a, f := settingsFixture(t)
			ctx := context.Background()
			original := map[string]string{}
			for key, value := range f.values {
				original[key] = value
			}
			var choices []string
			for _, control := range a.systemSettingsStatus(ctx, defaultConfig()) {
				if control.ID == definition.ID {
					choices = control.Choices
				}
			}
			for _, choice := range choices {
				if choice == "stock" {
					continue
				}
				r := a.Dispatch(ctx, Request{Action: "system_setting_set", ID: definition.ID, Choice: choice})
				if !r.OK {
					t.Fatal(r)
				}
				must(t, a.Step(ctx, "", false))
			}
			r := a.Dispatch(ctx, Request{Action: "system_setting_set", ID: definition.ID, Choice: "stock"})
			if !r.OK {
				t.Fatal(r)
			}
			for _, target := range definition.Targets(definition.Choices[1]) {
				key := target.Namespace + ":" + target.Key
				if target.Field == "" && f.values[key] != original[key] {
					t.Fatal(key, f.values[key], original[key])
				}
			}
			if a.Dispatch(ctx, Request{Action: "system_setting_set", ID: definition.ID, Choice: "999"}).OK {
				t.Fatal("unknown choice accepted")
			}
		})
	}
}
func TestBatteryExactBoundaryTimeoutCacheAndBoot(t *testing.T) {
	pad := maxBatteryBytes - len(checkinSample) - len("9,10001,i,uid,\n")
	raw := checkinSample + "9,10001,i,uid," + strings.Repeat("a", pad) + "\n"
	if len(raw) != maxBatteryBytes {
		t.Fatal(len(raw))
	}
	if _, err := parseBatteryStats(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := parseBatteryStats(raw + " "); err == nil {
		t.Fatal("over limit accepted")
	}
	a, base := fixture(t)
	f := &batteryAndroid{base: base, raw: checkinSample}
	a.Runner = f
	job, err := a.startBatteryJob(true, func(string) error { return nil })
	must(t, err)
	must(t, a.RunBatteryJob(context.Background(), job["id"].(string)))
	_, err = a.startBatteryJob(false, func(string) error { t.Fatal("fresh cache started worker"); return nil })
	must(t, err)
	job, err = a.startBatteryJob(true, func(string) error { return nil })
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	must(t, a.RunBatteryJob(ctx, job["id"].(string)))
	status, err := a.batteryJobStatus("")
	must(t, err)
	if status["error"] != "battery-timeout" || f.calls != 2 {
		t.Fatal(status, f.calls)
	}
	must(t, atomicWrite(a.system("/proc/sys/kernel/random/boot_id"), []byte("new-boot"), 0600))
	launched := false
	_, err = a.startBatteryJob(false, func(string) error { launched = true; return nil })
	must(t, err)
	if !launched {
		t.Fatal("old boot cache reused")
	}
}
func TestWatcherBatteryCoverageDoesNotFillGapsOrCharge(t *testing.T) {
	a, base := fixture(t)
	f := &batteryAndroid{base: base, level: "80"}
	a.Runner = f
	ctx := context.Background()
	at := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	must(t, atomicWrite(a.system("/sys/class/power_supply/battery/status"), []byte("Discharging"), 0600))
	must(t, a.Observe(ctx, "", true, at))
	f.level = "79"
	must(t, a.Observe(ctx, "", true, at.Add(time.Minute)))
	f.level = "78"
	must(t, a.Observe(ctx, "", true, at.Add(10*time.Minute)))
	must(t, atomicWrite(a.system("/sys/class/power_supply/battery/status"), []byte("Charging"), 0600))
	f.level = "77"
	must(t, a.Observe(ctx, "", true, at.Add(11*time.Minute)))
	day := a.learner.History.Days["2026-10-26"]
	if day.BatteryOffSeconds != 60 || day.BatteryOffDrop != 1 || day.BatteryIntervals != 1 {
		t.Fatal(fmt.Sprint(day))
	}
	must(t, a.flushLearning(a.learner, at.Add(11*time.Minute)))
	if a.batteryObservation()["percent_per_hour"] != nil {
		t.Fatal("charging rate exposed")
	}
}
func TestBackupExportIsPublicAndRejectsBadContracts(t *testing.T) {
	a, _ := settingsFixture(t)
	ctx := context.Background()
	must(t, a.saveAI(aiConfig("https://example.test/v1", "chat_completions")))
	r := a.Dispatch(ctx, Request{Action: "settings_backup_export"})
	if !r.OK {
		t.Fatal(r)
	}
	path := r.Data.(map[string]string)["path"]
	b, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(path, filepath.FromSlash("Download/HNHouji")) || strings.Contains(string(b), "test-secret") || strings.Contains(string(b), "ai_profiles") {
		t.Fatal(path, string(b))
	}
	backup, err := a.settingsBackup()
	must(t, err)
	backup.Version = 99
	if _, err = a.previewSettings(ctx, Request{Mode: "import", Backup: &backup}); err == nil {
		t.Fatal("unknown backup version accepted")
	}
	backup.Version = 1
	backup.Settings.KernelLevels = map[string]string{"tcp_congestion": "reno"}
	if _, err = a.previewSettings(ctx, Request{Mode: "import", Backup: &backup}); err == nil {
		t.Fatal("unsupported backup level accepted")
	}
}

func TestUnknownPressureCannotAuthorizeKernelLevels(t *testing.T) {
	for _, psi := range []string{"full avg10=invalid", "full avg10=NaN", "full avg10=Inf", "full avg10=-1"} {
		a, _ := fixture(t)
		seedKnobs(t, a)
		must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte(psi), 0600))
		r := a.Dispatch(context.Background(), Request{Action: "kernel_level_set", Control: "swap_readahead", Level: "1"})
		if r.OK || r.Error != "proposal-pressure-unknown" {
			t.Fatal(psi, r)
		}
		knobValue(t, a, "vm.page-cluster", "3")
	}
}

func TestTemporaryPressureDoesNotLatchWriteFailure(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=3.0"), 0600))
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
	if a.writeFailureStamp != "" {
		t.Fatal("temporary pressure latched as a write failure")
	}
	must(t, atomicWrite(a.system("/proc/pressure/memory"), []byte("full avg10=0.0"), 0600))
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "0")
}

func TestVersionOneReceiptStillUndoesAfterMigration(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	c := defaultConfig()
	c.Version = 1
	c.KernelControls = map[string]bool{"swap_readahead": false}
	must(t, atomicJSON(a.state("config.json"), c))
	must(t, atomicJSON(a.state("ai-application.json"), AIApplication{ProposalID: "legacy", Status: "applied", Changes: []AIChange{{Kind: "kernel_control", Control: "swap_readahead", Before: "on", After: "off", Reason: "legacy"}}}))
	must(t, a.migrateConfiguration())
	if a.ApplicationStatus().ProposalID != "legacy" {
		t.Fatal("migration lost receipt")
	}
	_, err := a.undoAI(ctx)
	must(t, err)
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "0")
}
