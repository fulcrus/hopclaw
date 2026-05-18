//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	out, statErr := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if statErr == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
		return false
	}
	return true
}

func terminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
