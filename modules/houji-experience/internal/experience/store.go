package experience

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ModuleID = "houji_experience"
const Version = "0.1.2-preview"

type Config struct {
	DefaultsVersion int               `json:"defaults_version"`
	Generation      int64             `json:"generation"`
	Version         int               `json:"version"`
	Enabled         bool              `json:"enabled"`
	KernelTuning    bool              `json:"kernel_tuning"`
	Apps            map[string]string `json:"apps"`
	Protected       map[string]bool   `json:"protected"`
	MigrationBoot   string            `json:"migration_boot,omitempty"`
	LegacySHA256    string            `json:"legacy_sha256,omitempty"`
}

func defaultConfig() Config {
	return Config{Version: 1, DefaultsVersion: 1, Enabled: true, KernelTuning: true, Apps: map[string]string{}, Protected: map[string]bool{}}
}

func disabledConfig() Config { c := defaultConfig(); c.Enabled = false; return c }

type Entry struct {
	Kind     string `json:"kind"`
	Package  string `json:"package"`
	Before   string `json:"before"`
	Applied  string `json:"applied"`
	Conflict bool   `json:"conflict,omitempty"`
}

type Journal struct {
	Boot    string  `json:"boot"`
	Entries []Entry `json:"entries"`
}

type Paths struct{ State, Module, Root, Legacy, Hook string }

func DefaultPaths() Paths {
	return Paths{State: "/data/adb/houji-experience", Module: "/data/adb/modules/" + ModuleID, Root: "/", Legacy: "/data/adb/service.d/houji-notification-helper.sh", Hook: "/data/adb/service.d/houji-experience-cleanup.sh"}
}

func (a *App) state(name string) string { return filepath.Join(a.Paths.State, name) }
func (a *App) system(name string) string {
	return filepath.Join(a.Paths.Root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	// Persist the directory entry on Android/Linux as well as file contents.
	if d, e := os.Open(filepath.Dir(path)); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 256*1024 {
		return errors.New("state too large")
	}
	return json.Unmarshal(data, value)
}

func (a *App) config() (Config, error) {
	c := Config{Version: 1, KernelTuning: true, Apps: map[string]string{}, Protected: map[string]bool{}}
	err := readJSON(a.state("config.json"), &c)
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if c.Apps == nil {
		c.Apps = map[string]string{}
	}
	if c.Protected == nil {
		c.Protected = map[string]bool{}
	}
	if err == nil && c.Version != 1 {
		err = errors.New("unsupported configuration version")
	}
	return c, err
}

func (a *App) journal() (Journal, error) {
	j := Journal{Entries: []Entry{}}
	err := readJSON(a.state("journal.json"), &j)
	if os.IsNotExist(err) {
		err = nil
	}
	return j, err
}

func (a *App) bootID() string {
	b, _ := os.ReadFile(a.system("/proc/sys/kernel/random/boot_id"))
	return strings.TrimSpace(string(b))
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// Directory ownership is fixed by lifecycle scripts. Never source configuration as shell.
func (a *App) lock(name string) (func(), error) {
	if err := os.MkdirAll(a.Paths.State, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(a.Paths.State, 0700); err != nil {
		return nil, err
	}
	path := a.state(name + ".lock")
	deadline := time.Now().Add(4 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = f.WriteString(fmt.Sprintf("%d %s", os.Getpid(), a.bootID()))
			_ = f.Close()
			if err != nil {
				_ = os.Remove(path)
				return nil, err
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		b, _ := os.ReadFile(path)
		fields := strings.Fields(string(b))
		pid := 0
		if len(fields) > 0 {
			pid, _ = strconv.Atoi(fields[0])
		}
		info, _ := os.Stat(path)
		oldBoot := len(fields) > 1 && fields[1] != a.bootID()
		if oldBoot || (pid > 0 && !processAlive(pid)) || (pid == 0 && info != nil && time.Since(info.ModTime()) > 5*time.Second) {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("busy: operation already running")
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func (a *App) logEvent(event string) {
	// Events are fixed codes, never user text, credentials, API response bodies or prompts.
	path := a.state("events.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 64*1024 {
		_ = os.Rename(path, a.state("events.previous.log"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), event)
		_ = f.Close()
	}
}

func digest(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
