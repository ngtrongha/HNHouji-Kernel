package experience

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}
type AndroidRunner struct{}

func (AndroidRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// No shell expansion, PATH lookup or arbitrary executable supplied by WebUI.
	cmd := exec.CommandContext(ctx, "/system/bin/"+name, args...)
	b, err := cmd.Output()
	if len(b) > 2*1024*1024 {
		return "", errors.New("system response too large")
	}
	return strings.TrimSpace(string(b)), err
}

type App struct {
	Paths              Paths
	Runner             Runner
	protectedStamp     string
	writeFailureStamp  string
	writeFailureReason string
	learner            *Learner
	dailyBusy          atomic.Bool
	dailyChecked       time.Time
	dailyPurgeAt       time.Time
}

func New(paths Paths, runner Runner) *App { return &App{Paths: paths, Runner: runner} }

var packageRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)
var profiles = map[string]bool{"daily": true, "native-game": true, "windows-game": true, "linux": true}

func validPackage(p string) bool { return len(p) <= 200 && packageRE.MatchString(p) }
func (a *App) run(ctx context.Context, name string, args ...string) (string, error) {
	return a.Runner.Run(ctx, name, args...)
}

func (a *App) compatible(ctx context.Context) error {
	d, err := a.run(ctx, "getprop", "ro.product.device")
	if err != nil || d != "houji" {
		return errors.New("requires Xiaomi 14 houji")
	}
	abi, err := a.run(ctx, "getprop", "ro.product.cpu.abi")
	if err != nil || abi != "arm64-v8a" {
		return errors.New("requires arm64-v8a")
	}
	k, err := a.run(ctx, "uname", "-r")
	if err != nil || !strings.HasPrefix(k, "6.1.") {
		return errors.New("requires GKI 6.1")
	}
	u, err := a.run(ctx, "id", "-u")
	if err != nil || u != "0" {
		return errors.New("requires root")
	}
	if !exists(a.system("/data/adb/ksu/bin/busybox")) {
		return errors.New("KernelSU BusyBox unavailable")
	}
	return nil
}

func (a *App) ownerUser(ctx context.Context) bool {
	u, e := a.run(ctx, "am", "get-current-user")
	return e == nil && u == "0"
}
func (a *App) installed(ctx context.Context, p string) bool {
	if !validPackage(p) {
		return false
	}
	out, e := a.run(ctx, "pm", "path", "--user", "0", p)
	return e == nil && strings.HasPrefix(out, "package:")
}

func (a *App) Packages(ctx context.Context) ([]string, error) {
	out, e := a.run(ctx, "pm", "list", "packages", "--user", "0")
	if e != nil {
		return nil, e
	}
	items := []string{}
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimPrefix(strings.TrimSpace(line), "package:")
		if validPackage(p) {
			items = append(items, p)
		}
	}
	return items, nil
}

var componentRE = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)/[A-Za-z0-9_.$]+`)

func foregroundFrom(out string, markers ...string) string {
	found := ""
	for _, line := range strings.Split(out, "\n") {
		for _, marker := range markers {
			if strings.Contains(line, marker) {
				m := componentRE.FindStringSubmatch(line)
				if len(m) > 1 {
					if found != "" && found != m[1] {
						return ""
					}
					found = m[1]
				}
			}
		}
	}
	return found
}

func (a *App) foreground(ctx context.Context) (string, bool) {
	power, e := a.run(ctx, "dumpsys", "power")
	if e != nil {
		return "", false
	}
	if strings.Contains(power, "mWakefulness=Asleep") || strings.Contains(power, "mWakefulness=Dozing") {
		return "", true
	}
	if !strings.Contains(power, "mWakefulness=Awake") {
		return "", false
	}
	window, e := a.run(ctx, "dumpsys", "window", "windows")
	if e != nil {
		return "", false
	}
	if strings.Contains(window, "mShowingLockscreen=true") || strings.Contains(window, "isStatusBarKeyguard=true") || strings.Contains(window, "mDreamingLockscreen=true") {
		return "", true
	}
	if strings.Contains(window, "mWindowingMode=multi-window") || strings.Contains(window, "mWindowingMode=split-screen") || strings.Contains(window, "mWindowingMode=freeform") {
		return "", false
	}
	activity, e := a.run(ctx, "dumpsys", "activity", "activities")
	if e == nil {
		if strings.Contains(activity, "topResumedActivity=") {
			return foregroundFrom(activity, "topResumedActivity="), false
		}
		if strings.Contains(activity, "mResumedActivity:") {
			return foregroundFrom(activity, "mResumedActivity:"), false
		}
	}
	return foregroundFrom(window, "mCurrentFocus="), false
}

func (a *App) current(ctx context.Context, e Entry) (string, error) {
	switch e.Kind {
	case "system":
		return a.currentSystem(ctx, e.Package)
	case "kernel":
		return a.readKernelKnob(e.Package)
	case "game":
		if !a.ownerUser(ctx) {
			return "", errors.New("game query requires owner user")
		}
		o, err := a.run(ctx, "cmd", "game", "list-modes", e.Package)
		if err != nil {
			return "", err
		}
		m := regexp.MustCompile(`current mode:\s*(standard|performance|battery|custom)\b`).FindStringSubmatch(o)
		if len(m) != 2 {
			return "", errors.New("game state unsupported")
		}
		return m[1], nil
	case "doze":
		o, err := a.run(ctx, "cmd", "deviceidle", "whitelist")
		if err != nil {
			return "", err
		}
		if strings.Contains(o, "Error:") || strings.Contains(o, "Exception") {
			return "", errors.New("whitelist unreadable")
		}
		for _, line := range strings.Split(o, "\n") {
			parts := strings.Split(strings.TrimSpace(line), ",")
			if len(parts) >= 2 && parts[1] == e.Package {
				return "true", nil
			}
		}
		return "false", nil
	case "millet":
		return a.run(ctx, "settings", "--user", "0", "get", "system", "MILLET_NO_RESTRICT_APP")
	}
	return "", errors.New("unknown journal entry")
}

func (a *App) writeValue(ctx context.Context, e Entry, value string) error {
	var out string
	var err error
	switch e.Kind {
	case "system":
		return a.writeSystem(ctx, e.Package, value)
	case "kernel":
		return a.writeKernelKnob(e.Package, value)
	case "game":
		if !validPackage(e.Package) || !map[string]bool{"standard": true, "performance": true, "battery": true, "custom": true}[value] {
			return errors.New("invalid game entry")
		}
		out, err = a.run(ctx, "cmd", "game", "mode", "--user", "0", value, e.Package)
	case "doze":
		if !validPackage(e.Package) || (value != "true" && value != "false") {
			return errors.New("invalid doze entry")
		}
		op := "-"
		if value == "true" {
			op = "+"
		}
		out, err = a.run(ctx, "cmd", "deviceidle", "whitelist", op+e.Package)
	case "millet":
		if value == "null" {
			out, err = a.run(ctx, "settings", "--user", "0", "delete", "system", "MILLET_NO_RESTRICT_APP")
		} else {
			out, err = a.run(ctx, "settings", "--user", "0", "put", "system", "MILLET_NO_RESTRICT_APP", value)
		}
	default:
		return errors.New("invalid journal kind")
	}
	if err != nil || strings.Contains(out, "Error:") || strings.Contains(out, "Exception") {
		return errors.New("Android rejected setting")
	}
	return nil
}

func (a *App) readSystem(path string) string {
	b, _ := os.ReadFile(a.system(path))
	return strings.TrimSpace(string(b))
}
