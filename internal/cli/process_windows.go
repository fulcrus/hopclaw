//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false
	}
	output := strings.TrimSpace(string(out))
	if output == "" || strings.Contains(output, "No tasks are running") {
		return false
	}
	return strings.Contains(output, fmt.Sprintf("\"%d\"", pid))
}

func terminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
