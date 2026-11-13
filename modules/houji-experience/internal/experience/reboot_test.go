package experience

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKernelTuningPausesOnlyWithCrashAndPriorKernelWrites(t *testing.T) {
	for _, tc := range []struct {
		name, system, loader, want string
		write, reboot              bool
	}{
		{"panic", "kernel_panic,subsystem", "hard", "kernel_panic", true, true},
		{"watchdog from bootloader", "", "watchdog,bark", "watchdog", true, true},
		{"refined reason wins", "reboot,userrequested", "watchdog", "", true, true},
		{"same boot", "kernel_panic", "", "", true, false},
		{"no prior writes", "watchdog", "", "", false, true},
		{"unknown reboot", "", "hard", "", true, true},
		{"userspace watchdog", "reboot,watchdog,system_server", "", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := fixture(t)
			seedKnobs(t, a)
			ctx := context.Background()
			if tc.write {
				must(t, a.Step(ctx, "", false))
				knobValue(t, a, "vm.page-cluster", "0")
			}
			f.properties = map[string]string{"sys.boot.reason": tc.system, "ro.boot.bootreason": tc.loader}
			if tc.reboot {
				must(t, atomicWrite(a.system("/proc/sys/kernel/random/boot_id"), []byte("boot-two"), 0600))
				must(t, atomicWrite(a.system("/proc/sys/vm/page-cluster"), []byte("2"), 0600))
				// This boot's stock value can coincide with the prior target.
				must(t, atomicWrite(a.system("/proc/sys/net/ipv4/tcp_congestion_control"), []byte("bbr"), 0600))
			}
			must(t, a.CleanupBoot(ctx))
			if a.kernelPauseReason() != "" {
				t.Fatal("early cleanup ran before Android resolved the reason")
			}
			must(t, a.Recover(ctx))
			if got := a.kernelPauseReason(); got != tc.want {
				t.Fatalf("pause = %q, want %q", got, tc.want)
			}
			if tc.reboot {
				knobValue(t, a, "vm.page-cluster", "2")
				knobValue(t, a, "tcp.congestion", "bbr")
			}
			must(t, a.Step(ctx, "", false))
			if tc.want == "" {
				knobValue(t, a, "vm.page-cluster", "0")
				return
			}
			knobValue(t, a, "vm.page-cluster", "2")
			if r := a.Dispatch(ctx, Request{Action: "kernel_apply"}); r.Error != "kernel-crash-paused" {
				t.Fatal(r)
			}
			must(t, atomicWrite(a.system("/proc/sys/kernel/random/boot_id"), []byte("boot-three"), 0600))
			must(t, a.Recover(ctx))
			must(t, a.Step(ctx, "", false))
			knobValue(t, a, "vm.page-cluster", "2")
			f.user = "10"
			if r := a.Dispatch(ctx, Request{Action: "kernel_tuning_set", Enabled: true}); r.Error != "owner-user-only" {
				t.Fatal(r)
			}
			f.user = "0"
			if r := a.Dispatch(ctx, Request{Action: "kernel_tuning_set", Enabled: true}); !r.OK {
				t.Fatal(r)
			}
			must(t, a.Step(ctx, "", false))
			knobValue(t, a, "vm.page-cluster", "0")
		})
	}
}

func TestBootDiagnosticsKeepsBoundedLogsOutOfReports(t *testing.T) {
	a, f := fixture(t)
	ctx := context.Background()
	f.properties = map[string]string{"sys.boot.reason": "kernel_panic", "ro.boot.bootreason": "hard", "ro.build.version.incremental": "OS3.example"}
	if _, state := a.ramoopsLogs(); state != "unavailable" {
		t.Fatal(state)
	}
	must(t, atomicWrite(a.system("/sys/fs/pstore/pmsg-ramoops-0"), []byte("app log"), 0600))
	if _, state := a.ramoopsLogs(); state != "no-records" {
		t.Fatal(state)
	}
	must(t, atomicWrite(a.system("/sys/fs/pstore/dmesg-ramoops-0"), nil, 0600))
	if _, state := a.ramoopsLogs(); state != "unreadable" {
		t.Fatal(state)
	}
	const raw = "private panic trace"
	must(t, atomicWrite(a.system("/sys/fs/pstore/dmesg-ramoops-0"), []byte(raw+strings.Repeat("a", 16384)), 0600))
	result := a.Dispatch(ctx, Request{Action: "boot_diagnostics"})
	if !result.OK {
		t.Fatal(result)
	}
	data := result.Data.(map[string]any)
	logs := data["logs"].([]rebootLog)
	if data["pstore_state"] != "available" || data["system_boot_reason"] != "kernel_panic" || data["firmware"] != "OS3.example" ||
		data["current_kernel_build_id"] == "" || len(logs) != 1 || !logs[0].Truncated ||
		len(logs[0].Text) != 16384 || !strings.HasPrefix(logs[0].Text, raw) {
		t.Fatal("missing boot evidence or failed log limit")
	}
	path, err := a.Export(ctx)
	must(t, err)
	exported, err := os.ReadFile(path)
	must(t, err)
	for _, view := range []string{string(exported), pretty(a.advisorContext(ctx, time.Now())), pretty(a.Status(ctx))} {
		if strings.Contains(view, raw) || strings.Contains(view, "pmsg-ramoops") || strings.Contains(view, "bootloader_boot_reason") {
			t.Fatal("diagnostics leaked outside the explicit read")
		}
	}
	f.user = "10"
	if r := a.Dispatch(ctx, Request{Action: "boot_diagnostics"}); r.Error != "owner-user-only" {
		t.Fatal(r)
	}
}
