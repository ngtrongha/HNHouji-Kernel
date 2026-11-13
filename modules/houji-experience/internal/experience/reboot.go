package experience

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const kernelPauseFile = "kernel-crash-pause.json"

func (a *App) kernelPauseReason() string {
	var reason string
	err := readJSON(a.state(kernelPauseFile), &reason)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil || (reason != "kernel_panic" && reason != "watchdog") {
		return "state-invalid"
	}
	return reason
}

func (a *App) bootProperty(ctx context.Context, name string) string {
	value, err := a.run(ctx, "getprop", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// The journal is written before each kernel write. It establishes exposure, not cause.
func (a *App) pauseAfterCrash(ctx context.Context, j Journal, boot string) error {
	if boot == "" || j.Boot == "" || j.Boot == boot || a.kernelPauseReason() != "" {
		return nil
	}
	for _, entry := range j.Entries {
		if entry.Kind != "kernel" || kernelControlForKnob(entry.Package) == "" {
			continue
		}
		reason := a.bootProperty(ctx, "sys.boot.reason")
		if reason == "" {
			reason = a.bootProperty(ctx, "ro.boot.bootreason")
		}
		reason, _, _ = strings.Cut(reason, ",")
		if reason == "kernel_panic" || reason == "watchdog" {
			return atomicJSON(a.state(kernelPauseFile), reason)
		}
		break
	}
	return nil
}

var ramoopsLog = regexp.MustCompile(`^(?:dmesg|console)-ramoops(?:-[0-9]+)?$`)

type rebootLog struct {
	Name      string `json:"name"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (a *App) bootDiagnostics(ctx context.Context) (map[string]any, error) {
	if !a.ownerUser(ctx) {
		return nil, errors.New("owner-user-only")
	}
	logs, state := a.ramoopsLogs()
	sync := a.KernelSync(ctx)
	return map[string]any{
		"system_boot_reason":      a.bootProperty(ctx, "sys.boot.reason"),
		"bootloader_boot_reason":  a.bootProperty(ctx, "ro.boot.bootreason"),
		"firmware":                a.bootProperty(ctx, "ro.build.version.incremental"),
		"current_kernel_release":  sync.KernelRelease,
		"current_kernel_build_id": sync.BuildID,
		"kernel_sync_state":       sync.State,
		"kernel_pause_reason":     a.kernelPauseReason(),
		"pstore_state":            state,
		"logs":                    logs,
	}, nil
}

func (a *App) ramoopsLogs() ([]rebootLog, string) {
	dir := a.system("/sys/fs/pstore")
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, "unavailable"
	}
	logs := []rebootLog{}
	state := "no-records"
	// shortcut: bounded text preview only, use adb if a full trace is needed.
	const limit = 16 * 1024
	for _, entry := range files {
		if !entry.Type().IsRegular() || !ramoopsLog.MatchString(entry.Name()) {
			continue
		}
		state = "unreadable"
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, limit+1))
		_ = f.Close()
		if err != nil || len(data) == 0 {
			continue
		}
		logs = append(logs, rebootLog{entry.Name(), string(data[:min(len(data), limit)]), len(data) > limit})
		if len(logs) == 4 {
			break
		}
	}
	if len(logs) > 0 {
		state = "available"
	}
	return logs, state
}
