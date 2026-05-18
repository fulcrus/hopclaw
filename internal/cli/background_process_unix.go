//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

func configureDetachedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
