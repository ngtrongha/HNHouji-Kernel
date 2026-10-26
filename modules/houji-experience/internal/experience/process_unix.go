//go:build !windows

package experience

import (
	"errors"
	"os/exec"
	"syscall"
)

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func startDetached(path, id string) error {
	return startDetachedCommand(path, "ai-worker", id)
}
func startDetachedCommand(path, command, id string) error {
	if command != "ai-worker" && command != "battery-worker" {
		return errors.New("unknown worker")
	}
	cmd := exec.Command(path, command, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
