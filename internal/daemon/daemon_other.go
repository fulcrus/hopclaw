//go:build !darwin && !linux && !windows

package daemon

import (
	"fmt"
	"runtime"
)

func newPlatformManager() (ServiceManager, error) {
	return nil, fmt.Errorf("daemon: unsupported platform %q", runtime.GOOS)
}
