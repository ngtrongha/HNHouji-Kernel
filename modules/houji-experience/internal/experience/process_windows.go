package experience

import (
	"errors"
	"syscall"
)

func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	return syscall.GetExitCodeProcess(h, &code) == nil && code == 259
}

func startDetached(path, id string) error { return errors.New("AI worker requires Android") }
