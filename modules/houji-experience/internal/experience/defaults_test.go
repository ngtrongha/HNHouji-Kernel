package experience

import (
	"context"
	"os"
	"testing"
)

func TestFreshDefaultIsDailyAndSkipsUnavailableKernelNodes(t *testing.T) {
	a, f := fixture(t)
	c, e := a.config()
	must(t, e)
	if !c.Enabled || !c.KernelTuning || c.DefaultsVersion != 1 || len(c.Apps) != 0 || len(c.Protected) != 0 {
		t.Fatal(c)
	}
	must(t, a.Step(context.Background(), "com.example.other", false))
	var r Runtime
	must(t, readJSON(a.state("runtime.json"), &r))
	if r.Profile != "daily" || r.Reason != "daily-default" || f.writes != 0 {
		t.Fatal(r, f.writes)
	}
	if a.snapshotConfig()["default_profile"] != "daily" {
		t.Fatal("default profile not exposed")
	}
}

func TestOldJSONEnablesKernelPresetAndExplicitFalsePersists(t *testing.T) {
	a, _ := fixture(t)
	must(t, atomicWrite(a.state("config.json"), []byte(`{"version":1,"enabled":false,"apps":{},"protected":{}}`), 0600))
	c, err := a.config()
	must(t, err)
	if !c.KernelTuning {
		t.Fatal("missing old field did not receive default")
	}
	c.KernelTuning = false
	must(t, atomicJSON(a.state("config.json"), c))
	c, err = a.config()
	must(t, err)
	if c.KernelTuning {
		t.Fatal("explicit false overridden")
	}
}

func installFixture(t *testing.T, a *App) {
	t.Helper()
	exe := a.state("test-binary")
	must(t, atomicWrite(exe, []byte("fixture-only"), 0700))
	must(t, a.Install(context.Background(), exe))
}

func TestUpgradeEmptyOldDefaultsEnablesDailyOnce(t *testing.T) {
	a, _ := fixture(t)
	old := Config{Version: 1, Enabled: false, Apps: map[string]string{}, Protected: map[string]bool{}}
	must(t, atomicJSON(a.state("config.json"), old))
	installFixture(t, a)
	c, e := a.config()
	must(t, e)
	if !c.Enabled || c.DefaultsVersion != 1 {
		t.Fatal(c)
	}
	if r := a.Dispatch(context.Background(), Request{Action: "disable"}); !r.OK {
		t.Fatal(r)
	}
	installFixture(t, a)
	c, e = a.config()
	must(t, e)
	if c.Enabled {
		t.Fatal("update overrode explicit disable")
	}
}

func TestUpgradePreservesConfiguredAppRulesAndDisabledState(t *testing.T) {
	a, _ := fixture(t)
	old := Config{Version: 1, Enabled: false, Apps: map[string]string{"com.example.game": "native-game"}, Protected: map[string]bool{"com.example.chat": true}}
	must(t, atomicJSON(a.state("config.json"), old))
	installFixture(t, a)
	c, e := a.config()
	must(t, e)
	if c.Enabled || c.Apps["com.example.game"] != "native-game" || !c.Protected["com.example.chat"] {
		t.Fatal(c)
	}
}

func TestLegacyMigrationWaitsForRebootThenResumesDaily(t *testing.T) {
	a, f := fixture(t)
	must(t, atomicWrite(a.Paths.Legacy, []byte("old helper"), 0700))
	installFixture(t, a)
	c, e := a.config()
	must(t, e)
	if !c.Enabled || c.MigrationBoot != "boot-one" || exists(a.Paths.Legacy) {
		t.Fatal(c)
	}
	must(t, a.Step(context.Background(), "", false))
	var r Runtime
	must(t, readJSON(a.state("runtime.json"), &r))
	if r.Profile != "observe" || r.Reason != "reboot-required" || f.writes != 0 {
		t.Fatal(r)
	}
	must(t, os.WriteFile(a.system("/proc/sys/kernel/random/boot_id"), []byte("boot-two"), 0600))
	must(t, a.Recover(context.Background()))
	must(t, a.Step(context.Background(), "", false))
	must(t, readJSON(a.state("runtime.json"), &r))
	if r.Profile != "daily" {
		t.Fatal(r)
	}
}
