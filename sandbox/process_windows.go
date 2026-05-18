//go:build windows

package sandbox

import (
	"syscall"
)

// ---------------------------------------------------------------------------
// Windows resource limits (platform limitation: no rlimits/pdeathsig)
// ---------------------------------------------------------------------------

// buildSysProcAttr returns a no-op SysProcAttr on Windows. Full process
// isolation (rlimits, pdeathsig) is not supported on this platform. Timeouts
// and output limits are still enforced by the ProcessRunner.
func buildSysProcAttr(cfg ProcessConfig) *syscall.SysProcAttr {
	log.Warn("sandbox: process isolation is limited on windows, only timeout and output limits are enforced")
	return &syscall.SysProcAttr{}
}
