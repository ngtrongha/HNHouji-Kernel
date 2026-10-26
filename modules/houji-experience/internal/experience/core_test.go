package experience

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeAndroid struct {
	game, millet, power, activity, window, user, failWrite, failRead, release string
	doze                                                                      map[string]bool
	writes                                                                    int
	commands                                                                  []string
}

func (f *fakeAndroid) Run(_ context.Context, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, cmd)
	if f.failRead != "" && strings.Contains(cmd, f.failRead) {
		return "", errors.New("read timeout")
	}
	switch name {
	case "getprop":
		switch args[0] {
		case "ro.product.device":
			return "houji", nil
		case "ro.product.cpu.abi":
			return "arm64-v8a", nil
		case "ro.build.version.sdk":
			return "36", nil
		}
		return "16", nil
	case "uname":
		if f.release != "" {
			return f.release, nil
		}
		return "6.1.177-android14-r11", nil
	case "id":
		return "0", nil
	case "am":
		if args[0] == "get-current-user" {
			return f.user, nil
		}
		return "", nil
	case "pm":
		if args[0] == "path" {
			return "package:/data/app/base.apk", nil
		}
		return "package:com.example.game\npackage:com.example.chat\n", nil
	case "dumpsys":
		switch args[0] {
		case "power":
			return f.power, nil
		case "activity":
			return f.activity, nil
		case "window":
			return f.window, nil
		case "battery":
			return "level: 80\ntemperature: 350", nil
		case "thermalservice":
			return "Thermal Status: 0", nil
		}
	case "settings":
		if args[2] == "list" {
			return "", nil
		}
		if args[2] == "get" {
			return f.millet, nil
		}
		f.writes++
		if args[2] == "delete" {
			f.millet = "null"
		} else {
			f.millet = args[len(args)-1]
		}
		return "", nil
	case "cmd":
		if args[0] == "game" {
			switch args[1] {
			case "help":
				return " mode list-modes", nil
			case "list-modes":
				return args[2] + " current mode: " + f.game + ", available game modes: [standard,performance,battery]", nil
			case "mode":
				f.writes++
				f.game = args[len(args)-2]
				if f.failWrite == "game" {
					return "", errors.New("write interrupted")
				}
				return "", nil
			}
		}
		if args[0] == "deviceidle" {
			if len(args) == 2 {
				out := ""
				for p, on := range f.doze {
					if on {
						out += "user," + p + ",10001\n"
					}
				}
				return out, nil
			}
			f.writes++
			p := args[2][1:]
			f.doze[p] = args[2][0] == '+'
			return "", nil
		}
	}
	return "", errors.New("unsupported fake command: " + cmd)
}

func fixture(t *testing.T) (*App, *fakeAndroid) {
	t.Helper()
	root := t.TempDir()
	paths := Paths{State: filepath.Join(root, "state"), Module: filepath.Join(root, "module"), Root: root, Legacy: filepath.Join(root, "service.d", "legacy.sh"), Hook: filepath.Join(root, "service.d", "cleanup.sh")}
	if e := os.MkdirAll(paths.Module, 0700); e != nil {
		t.Fatal(e)
	}
	f := &fakeAndroid{game: "standard", millet: "com.stock.app", doze: map[string]bool{}, user: "0", power: "mWakefulness=Awake", window: "mCurrentFocus=Window{ com.example.game/.Main }", activity: "topResumedActivity=ActivityRecord{ com.example.game/.Main }"}
	a := New(paths, f)
	seedKernel(t, a)
	for p, data := range map[string]string{"/proc/sys/kernel/random/boot_id": "boot-one", "/data/adb/ksu/bin/busybox": "mock", "/proc/meminfo": "MemTotal: 8000000 kB\nMemAvailable: 3000000 kB", "/proc/pressure/memory": "some avg10=0.10 avg60=0.0\nfull avg10=0.00 avg60=0.0"} {
		if e := atomicWrite(a.system(p), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return a, f
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func configured(t *testing.T, a *App, protected bool) {
	t.Helper()
	c, _ := a.config()
	c.Enabled = true
	c.Apps["com.example.game"] = "native-game"
	if protected {
		c.Protected["com.example.chat"] = true
	}
	must(t, atomicJSON(a.state("config.json"), c))
}

func TestGameTransitionRestores(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "performance" {
		t.Fatal(f.game)
	}
	must(t, a.Step(ctx, "com.example.home", false))
	if f.game != "standard" {
		t.Fatal(f.game)
	}
	j, _ := a.journal()
	if len(j.Entries) != 0 {
		t.Fatal(j)
	}
}
func TestRestorePreservesExternalChanges(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	f.game = "battery"
	if a.restoreAll(ctx) == nil {
		t.Fatal("expected conflict")
	}
	if f.game != "battery" {
		t.Fatal("overwrote external change")
	}
	j, _ := a.journal()
	if !j.Entries[0].Conflict {
		t.Fatal(j)
	}
}
func TestCrashAfterWriteRecovered(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	j, _ := a.journal()
	f.failWrite = "game"
	if a.apply(ctx, &j, Entry{Kind: "game", Package: "com.example.game", Applied: "performance"}) == nil {
		t.Fatal("expected interruption")
	}
	f.failWrite = ""
	must(t, a.restoreAll(ctx))
	if f.game != "standard" {
		t.Fatal(f.game)
	}
}
func TestJournalBeforeWriteRecovery(t *testing.T) {
	a, f := fixture(t)
	j := Journal{Boot: "old", Entries: []Entry{{Kind: "game", Package: "com.example.game", Before: "standard", Applied: "performance"}}}
	must(t, atomicJSON(a.state("journal.json"), j))
	must(t, a.Recover(context.Background()))
	if f.writes != 0 {
		t.Fatal("wrote despite crash before apply")
	}
	j, _ = a.journal()
	if len(j.Entries) != 0 {
		t.Fatal(j)
	}
}
func TestProtectionAddsOnlyOwnedExceptions(t *testing.T) {
	a, f := fixture(t)
	f.doze["com.stock.app"] = true
	configured(t, a, true)
	ctx := context.Background()
	must(t, a.Step(ctx, "", false))
	if !f.doze["com.example.chat"] || f.millet != "com.stock.app,com.example.chat" {
		t.Fatal(f)
	}
	r := a.Dispatch(ctx, Request{Action: "disable"})
	if !r.OK {
		t.Fatal(r)
	}
	if f.doze["com.example.chat"] || !f.doze["com.stock.app"] || f.millet != "com.stock.app" {
		t.Fatal("incorrect restoration", f)
	}
}
func TestUnknownMILLETNotCreated(t *testing.T) {
	a, f := fixture(t)
	f.millet = "null"
	configured(t, a, true)
	must(t, a.Step(context.Background(), "", false))
	if f.millet != "null" {
		t.Fatal(f.millet)
	}
}
func TestManagerDisableAndBootHookRestore(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, true)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	must(t, atomicWrite(filepath.Join(a.Paths.Module, "disable"), nil, 0600))
	must(t, a.CleanupBoot(ctx))
	if f.game != "standard" || f.doze["com.example.chat"] {
		t.Fatal("not restored")
	}
	c, _ := a.config()
	if c.Enabled {
		t.Fatal("still enabled")
	}
}
func TestLegacyMigrationAndConflict(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	must(t, atomicWrite(a.Paths.Legacy, []byte("#!/system/bin/sh\necho old\n"), 0700))
	must(t, a.Step(ctx, "com.example.game", false))
	if f.game != "standard" {
		t.Fatal("legacy conflict left game override")
	}
	r := a.Dispatch(ctx, Request{Action: "migrate_legacy"})
	if !r.OK {
		t.Fatal(r)
	}
	if exists(a.Paths.Legacy) {
		t.Fatal("legacy still active")
	}
	r = a.Dispatch(ctx, Request{Action: "enable"})
	if r.OK || r.Error != "reboot-required" {
		t.Fatal(r)
	}
	must(t, atomicWrite(a.system("/proc/sys/kernel/random/boot_id"), []byte("boot-two"), 0600))
	if r = a.Dispatch(ctx, Request{Action: "enable"}); !r.OK {
		t.Fatal(r)
	}
	c, _ := a.config()
	if !exists(a.state("legacy-" + c.LegacySHA256 + ".sh")) {
		t.Fatal("backup absent")
	}
}
func TestRejectShellInputBeforeAndroidCommands(t *testing.T) {
	a, f := fixture(t)
	for _, p := range []string{"com.app;reboot", "$(id)", "com.app\nreboot", "../com.app", "com.app'"} {
		before := len(f.commands)
		r := a.Dispatch(context.Background(), Request{Action: "app_set", Package: p, Profile: "native-game"})
		if r.OK || len(f.commands) != before {
			t.Fatal("unvalidated input reached runner", p, r)
		}
	}
}
func TestScreenOffAndAmbiguousForeground(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	pkg, off := a.foreground(ctx)
	if pkg != "com.example.game" || off {
		t.Fatal(pkg, off)
	}
	f.activity += "\ntopResumedActivity=ActivityRecord{ com.example.other/.Main }"
	pkg, _ = a.foreground(ctx)
	if pkg != "" {
		t.Fatal("ambiguous foreground used", pkg)
	}
	f.power = "mWakefulness=Asleep"
	pkg, off = a.foreground(ctx)
	if pkg != "" || !off {
		t.Fatal(pkg, off)
	}
	f.power = "mWakefulness=Awake"
	f.window = "mShowingLockscreen=true"
	pkg, off = a.foreground(ctx)
	if pkg != "" || !off {
		t.Fatal(pkg, off)
	}
}
func TestNoWritesForUnconfiguredModule(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	a.Report(ctx)
	a.Doctor(ctx)
	if f.writes != 0 {
		t.Fatal("unconfigured module changed settings")
	}
}
func TestUnknownGameModeAndOwnerUser(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	f.failRead = "game list-modes"
	must(t, a.Step(context.Background(), "com.example.game", false))
	if f.writes != 0 {
		t.Fatal("unsupported mode written")
	}
	f.failRead = ""
	f.user = "10"
	must(t, a.Step(context.Background(), "com.example.game", false))
	if f.writes != 0 {
		t.Fatal("secondary user written")
	}
}
func TestIdleStepDoesNotRewriteJournal(t *testing.T) {
	a, _ := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	before, _ := os.Stat(a.state("journal.json"))
	must(t, a.Step(ctx, "com.example.game", false))
	after, _ := os.Stat(a.state("journal.json"))
	if before.ModTime() != after.ModTime() {
		t.Fatal("poll rewrote journal")
	}
}
func TestCorruptConfigFailsClosed(t *testing.T) {
	a, f := fixture(t)
	must(t, atomicWrite(a.state("config.json"), []byte("bad json"), 0600))
	if a.Step(context.Background(), "com.example.game", false) == nil || f.writes != 0 {
		t.Fatal("corrupt state applied")
	}
}
func TestLockAndStaleLock(t *testing.T) {
	a, _ := fixture(t)
	unlock, e := a.lock("state")
	must(t, e)
	if _, e = os.OpenFile(a.state("state.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); !os.IsExist(e) {
		t.Fatal(e)
	}
	unlock()
	must(t, atomicWrite(a.state("state.lock"), []byte("99999999"), 0600))
	unlock, e = a.lock("state")
	must(t, e)
	unlock()
}
func TestExportExcludesPackagesAndCredentials(t *testing.T) {
	a, _ := fixture(t)
	configured(t, a, true)
	must(t, a.saveAI(AIConfig{Enabled: true, Style: "chat_completions", BaseURL: "https://private.example/v1", Model: "secret-model", Auth: "bearer", Key: "SECRET"}))
	path, e := a.Export(context.Background())
	must(t, e)
	b, _ := os.ReadFile(path)
	for _, bad := range []string{"SECRET", "private.example", "secret-model", "com.example.chat"} {
		if strings.Contains(string(b), bad) {
			t.Fatal("export leaked", bad)
		}
	}
}

func TestRapidDisableEnableReappliesProtection(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, true)
	ctx := context.Background()
	must(t, a.Step(ctx, "", false))
	if r := a.Dispatch(ctx, Request{Action: "disable"}); !r.OK {
		t.Fatal(r)
	}
	if r := a.Dispatch(ctx, Request{Action: "enable"}); !r.OK {
		t.Fatal(r)
	}
	must(t, a.Step(ctx, "", false))
	if !f.doze["com.example.chat"] {
		t.Fatal("cached protection skipped after rapid re-enable")
	}
}
func TestCorruptConfigCanStillRestore(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	must(t, atomicWrite(a.state("config.json"), []byte("broken"), 0600))
	if a.Step(ctx, "com.example.game", false) == nil {
		t.Fatal("expected config error")
	}
	if f.game != "standard" {
		t.Fatal("corrupt config blocked restore")
	}
	if r := a.Dispatch(ctx, Request{Action: "disable"}); !r.OK {
		t.Fatal(r)
	}
	if !exists(a.state("config.invalid.backup")) {
		t.Fatal("missing broken config backup")
	}
}
func TestBootIdentityPreventsPIDReuseLock(t *testing.T) {
	a, _ := fixture(t)
	must(t, atomicWrite(a.state("state.lock"), []byte(fmt.Sprintf("%d old-boot", os.Getpid())), 0600))
	u, e := a.lock("state")
	must(t, e)
	u()
}
func TestAcknowledgeConflictPreservesNewSetting(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	f.game = "battery"
	if a.restoreAll(ctx) == nil {
		t.Fatal("expected conflict")
	}
	if r := a.Dispatch(ctx, Request{Action: "resolve_conflicts"}); !r.OK {
		t.Fatal(r)
	}
	if f.game != "battery" {
		t.Fatal("external mode overwritten")
	}
	j, _ := a.journal()
	if len(j.Entries) != 0 {
		t.Fatal(j)
	}
}
func TestUninstallKeepsRecoveryHookOnConflict(t *testing.T) {
	a, f := fixture(t)
	configured(t, a, false)
	ctx := context.Background()
	must(t, atomicWrite(a.Paths.Hook, []byte("mock"), 0700))
	must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
	must(t, a.Step(ctx, "com.example.game", false))
	f.game = "battery"
	if a.Uninstall(ctx) == nil {
		t.Fatal("expected conflict")
	}
	if !exists(a.Paths.Hook) || exists(a.state("ai.json")) {
		t.Fatal("recovery removed or credential retained")
	}
}

func TestUninstallCorruptConfigStillDeletesKey(t *testing.T) {
	a, _ := fixture(t)
	must(t, a.saveAI(aiConfig("https://example.test/v1", "responses")))
	must(t, atomicWrite(a.state("config.json"), []byte("broken"), 0600))
	must(t, a.Uninstall(context.Background()))
	if exists(a.state("ai.json")) {
		t.Fatal("credential remained")
	}
}
