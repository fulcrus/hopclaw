//go:build !windows && !linux

package sandbox

import (
	"syscall"
)

// ---------------------------------------------------------------------------
// Non-Linux Unix resource limits (macOS, FreeBSD, etc.)
// ---------------------------------------------------------------------------

// buildSysProcAttr returns a SysProcAttr with the subset of OS-level resource
// restrictions available on this platform. Pdeathsig and RLIMIT_NPROC are not
// available outside Linux, so only RLIMIT_FSIZE is applied.
func buildSysProcAttr(cfg ProcessConfig) *syscall.SysProcAttr {
	setResourceLimits(cfg)
	return &syscall.SysProcAttr{}
}

// setResourceLimits applies per-process resource limits using setrlimit.
// On non-Linux Unix, only RLIMIT_FSIZE is supported. Failures are logged
// but do not prevent execution.
func setResourceLimits(cfg ProcessConfig) {
	if cfg.MaxFileSize > 0 {
		limit := &syscall.Rlimit{
			Cur: uint64(cfg.MaxFileSize),
			Max: uint64(cfg.MaxFileSize),
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, limit); err != nil {
			log.Warn("sandbox: failed to set RLIMIT_FSIZE", "error", err)
		}
	}
}
