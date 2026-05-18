//go:build darwin

package desktopd

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// screenCaptureSettingsURL is the macOS deep-link to the Screen Recording
// privacy pane.  Opening this URL brings the user directly to the toggle
// where they can grant permission.
const screenCaptureSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"

var (
	screenPermOnce      sync.Once
	screenPermRequested bool
)

// ensureScreenCapturePermission checks macOS TCC screen-capture permission
// and, if absent, attempts to register the current process so it appears in
// System Settings → Privacy & Security → Screen Recording.
//
// macOS does NOT show a permission dialog for CLI processes — the user must
// toggle the switch manually.  This function opens the settings pane
// automatically on first denial so the user knows exactly what to do.
func ensureScreenCapturePermission(ctx context.Context) error {
	if bool(C.CGPreflightScreenCaptureAccess()) {
		return nil // already granted
	}

	// Request access.  For CLI processes this will NOT show a dialog but
	// WILL register the binary in the Screen Recording list so the user
	// can find and enable it.
	C.CGRequestScreenCaptureAccess()

	// Re-check after request.
	if bool(C.CGPreflightScreenCaptureAccess()) {
		return nil // user may have pre-approved
	}

	// Open the settings pane once per process lifetime so we don't spam
	// the user on every call.
	screenPermOnce.Do(func() {
		_ = exec.CommandContext(ctx, "open", screenCaptureSettingsURL).Run()
		screenPermRequested = true
	})

	return fmt.Errorf("screen recording permission denied: " +
		"macOS requires manual authorization for CLI processes. " +
		"System Settings → Privacy & Security → Screen Recording has been opened. " +
		"Please enable the toggle for hopclaw-desktopd (or the terminal running it), " +
		"then retry. The process may need to be restarted after granting permission")
}
