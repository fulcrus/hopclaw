//go:build linux

package sandbox

import (
	"syscall"
)

const linuxRlimitNProc = 6

// ---------------------------------------------------------------------------
// Linux resource limits
// ---------------------------------------------------------------------------

// buildSysProcAttr returns a SysProcAttr configured with Linux-specific
// resource restrictions: Pdeathsig ensures the child is killed when the parent
// exits, and rlimits constrain file size and process count.
func buildSysProcAttr(cfg ProcessConfig) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	setResourceLimits(cfg)

	return attr
}

// setResourceLimits applies per-process resource limits using setrlimit.
// Failures are logged but do not prevent execution.
func setResourceLimits(cfg ProcessConfig) {
	// RLIMIT_FSIZE — max file size the process can create.
	if cfg.MaxFileSize > 0 {
		limit := &syscall.Rlimit{
			Cur: uint64(cfg.MaxFileSize),
			Max: uint64(cfg.MaxFileSize),
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, limit); err != nil {
			log.Warn("sandbox: failed to set RLIMIT_FSIZE", "error", err)
		}
	}

	// RLIMIT_NPROC — max number of child processes.
	if cfg.MaxProcs > 0 {
		limit := &syscall.Rlimit{
			Cur: uint64(cfg.MaxProcs),
			Max: uint64(cfg.MaxProcs),
		}
		if err := syscall.Setrlimit(linuxRlimitNProc, limit); err != nil {
			log.Warn("sandbox: failed to set RLIMIT_NPROC", "error", err)
		}
	}
}
