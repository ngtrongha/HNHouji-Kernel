package experience

import (
	"context"
	"os"
	"strings"
	"testing"
)

func seedKnobs(t *testing.T, a *App) {
	t.Helper()
	for path, value := range map[string]string{
		"/proc/swaps":               "Filename\tType\tSize\tUsed\tPriority\n/dev/block/zram0 partition 8388604 1024 32767\n",
		"/proc/sys/vm/page-cluster": "3\n",
		"/proc/sys/net/ipv4/tcp_available_congestion_control": "reno cubic bbr\n",
		"/proc/sys/net/ipv4/tcp_congestion_control":           "cubic\n",
		"/proc/self/mountinfo":                                "100 99 253:42 / /data rw,nosuid - f2fs /dev/block/dm-42 rw\n",
		"/sys/class/block/dm-42/dev":                          "253:42\n",
		"/sys/fs/f2fs/dm-42/min_fsync_blocks":                 "8\n",
		"/sys/class/block/dm-7/dev":                           "253:7\n",
		"/sys/fs/f2fs/dm-7/min_fsync_blocks":                  "9\n",
	} {
		must(t, atomicWrite(a.system(path), []byte(value), 0600))
	}
}
func knobValue(t *testing.T, a *App, id, want string) {
	t.Helper()
	got, err := a.readKernelKnob(id)
	must(t, err)
	if got != want {
		t.Fatalf("%s: got %s want %s", id, got, want)
	}
}

func TestDailyTunesRealNodesAndRestoresOriginals(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.browser", false))
	for id, want := range map[string]string{"vm.page-cluster": "0", "tcp.congestion": "bbr", "f2fs:dm-42": "20", "f2fs:dm-7": "9"} {
		knobValue(t, a, id, want)
	}
	j, err := a.journal()
	must(t, err)
	if len(j.Entries) != 3 {
		t.Fatal(j)
	}
	before, err := os.ReadFile(a.state("journal.json"))
	must(t, err)
	must(t, a.Step(ctx, "", true))
	after, err := os.ReadFile(a.state("journal.json"))
	must(t, err)
	if string(before) != string(after) {
		t.Fatal("unchanged poll rewrote journal")
	}
	if r := a.Dispatch(ctx, Request{Action: "disable"}); !r.OK {
		t.Fatal(r)
	}
	for id, want := range map[string]string{"vm.page-cluster": "3", "tcp.congestion": "cubic", "f2fs:dm-42": "8"} {
		knobValue(t, a, id, want)
	}
	j, err = a.journal()
	must(t, err)
	if len(j.Entries) != 0 {
		t.Fatal(j)
	}
}

func TestNativeTuningSkipsUnsupportedOrAmbiguousCapabilities(t *testing.T) {
	for _, condition := range []string{"disk-swap", "no-swap", "no-bbr", "data-ext4", "ambiguous-data", "read-only-data"} {
		t.Run(condition, func(t *testing.T) {
			a, _ := fixture(t)
			seedKnobs(t, a)
			switch condition {
			case "disk-swap":
				must(t, os.WriteFile(a.system("/proc/swaps"), []byte("Filename Type Size Used Priority\n/dev/block/zram0 partition 1024 0 1\n/data/swapfile file 1024 0 -1\n"), 0600))
			case "no-swap":
				must(t, os.WriteFile(a.system("/proc/swaps"), []byte("Filename Type Size Used Priority\n"), 0600))
			case "no-bbr":
				must(t, os.WriteFile(a.system("/proc/sys/net/ipv4/tcp_available_congestion_control"), []byte("cubic reno"), 0600))
			case "data-ext4":
				must(t, os.WriteFile(a.system("/proc/self/mountinfo"), []byte("100 99 253:42 / /data rw - ext4 /dev/block/dm-42 rw"), 0600))
			case "ambiguous-data":
				must(t, os.WriteFile(a.system("/proc/self/mountinfo"), []byte("100 99 253:42 / /data rw - f2fs /dev/block/dm-42 rw\n101 99 253:7 / /data rw - f2fs /dev/block/dm-7 rw"), 0600))
			case "read-only-data":
				must(t, os.WriteFile(a.system("/proc/self/mountinfo"), []byte("100 99 253:42 / /data ro - f2fs /dev/block/dm-42 ro"), 0600))
			}
			must(t, a.Step(context.Background(), "", false))
			if strings.Contains(condition, "swap") {
				knobValue(t, a, "vm.page-cluster", "3")
			}
			if condition == "no-bbr" {
				knobValue(t, a, "tcp.congestion", "cubic")
			}
			if strings.Contains(condition, "data") {
				knobValue(t, a, "f2fs:dm-42", "8")
			}
		})
	}
}

func TestNativeKernelMismatchRestoresAndBlocksNewWrites(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	must(t, a.Step(ctx, "", false))
	must(t, os.Remove(a.system("/data/adb/houji-kernel/manifest.json")))
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
	knobValue(t, a, "tcp.congestion", "cubic")
	knobValue(t, a, "f2fs:dm-42", "8")
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
}

func TestExternalKernelWritePreservedAndNoRepeatedApply(t *testing.T) {
	for _, external := range []string{"1", "3"} {
		t.Run(external, func(t *testing.T) {
			a, _ := fixture(t)
			seedKnobs(t, a)
			ctx := context.Background()
			must(t, a.Step(ctx, "", false))
			must(t, a.writeKernelKnob("vm.page-cluster", external))
			if err := a.Step(ctx, "", false); err == nil {
				t.Fatal("external write ignored")
			}
			knobValue(t, a, "vm.page-cluster", external)
			knobValue(t, a, "tcp.congestion", "cubic")
			before, err := os.ReadFile(a.state("journal.json"))
			must(t, err)
			must(t, a.Step(ctx, "", false))
			after, err := os.ReadFile(a.state("journal.json"))
			must(t, err)
			if string(before) != string(after) {
				t.Fatal("failure loop rewrote journal")
			}
			knobValue(t, a, "vm.page-cluster", external)
			knobValue(t, a, "tcp.congestion", "cubic")
		})
	}
}

func TestKernelTuningToggleRestoresWithoutDisablingAppRules(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	configured(t, a, false)
	ctx := context.Background()
	must(t, a.Step(ctx, "com.example.game", false))
	r := a.Dispatch(ctx, Request{Action: "kernel_tuning_set", Enabled: false})
	if !r.OK {
		t.Fatal(r)
	}
	knobValue(t, a, "vm.page-cluster", "3")
	c, err := a.config()
	must(t, err)
	if !c.Enabled || c.KernelTuning {
		t.Fatal(c)
	}
	must(t, a.Step(ctx, "com.example.game", false))
	knobValue(t, a, "vm.page-cluster", "3")
	j, err := a.journal()
	must(t, err)
	if len(j.Entries) != 1 || j.Entries[0].Kind != "game" {
		t.Fatal(j)
	}
}

func TestKernelAllowlistRejectsPathsAndDoesNotCreateMissingNodes(t *testing.T) {
	a, _ := fixture(t)
	for _, id := range []string{"/proc/sys/vm/swappiness", "f2fs:../../proc", "f2fs:dm-42/other", "walt.window"} {
		if err := a.writeKernelKnob(id, "0"); err == nil {
			t.Fatal(id)
		}
	}
	for _, value := range []string{"-1", "32", "0; reboot", "1\n2"} {
		if err := a.writeKernelKnob("vm.page-cluster", value); err == nil {
			t.Fatal(value)
		}
	}
	if err := a.writeKernelKnob("vm.page-cluster", "0"); err == nil {
		t.Fatal("created missing node")
	}
	if exists(a.system("/proc/sys/vm/page-cluster")) {
		t.Fatal("missing node created")
	}
}

func TestAlreadyConfiguredKnobsNotOwnedAndRecoveryWorksWithoutModule(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	must(t, a.writeKernelKnob("tcp.congestion", "bbr"))
	must(t, a.Step(ctx, "", false))
	j, err := a.journal()
	must(t, err)
	if len(j.Entries) != 2 {
		t.Fatal(j)
	}
	must(t, os.Remove(a.Paths.Module))
	must(t, a.Recover(ctx))
	knobValue(t, a, "vm.page-cluster", "3")
	knobValue(t, a, "tcp.congestion", "bbr")
	knobValue(t, a, "f2fs:dm-42", "8")
}

func TestLostZRAMCapabilityRestoresPageCluster(t *testing.T) {
	a, _ := fixture(t)
	seedKnobs(t, a)
	ctx := context.Background()
	must(t, a.Step(ctx, "", false))
	must(t, os.WriteFile(a.system("/proc/swaps"), []byte("Filename Type Size Used Priority\n/data/swapfile file 1024 0 1\n"), 0600))
	must(t, a.Step(ctx, "", false))
	knobValue(t, a, "vm.page-cluster", "3")
	knobValue(t, a, "tcp.congestion", "bbr")
}

func TestAppProtectionFailureDoesNotRepeatedlyReapplyKernel(t *testing.T) {
	a, f := fixture(t)
	seedKnobs(t, a)
	configured(t, a, true)
	ctx := context.Background()
	f.failRead = "deviceidle whitelist"
	if err := a.Step(ctx, "", false); err == nil {
		t.Fatal("expected failed protection query")
	}
	knobValue(t, a, "vm.page-cluster", "3")
	knobValue(t, a, "tcp.congestion", "cubic")
	before, err := os.ReadFile(a.state("journal.json"))
	must(t, err)
	must(t, a.Step(ctx, "", false))
	after, err := os.ReadFile(a.state("journal.json"))
	must(t, err)
	if string(before) != string(after) {
		t.Fatal("app failure reapplied kernel preset")
	}
	knobValue(t, a, "vm.page-cluster", "3")
}
