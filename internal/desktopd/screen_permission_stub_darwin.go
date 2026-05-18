//go:build darwin && !cgo

package desktopd

import "context"

// CGO-free release builds cannot call CoreGraphics permission APIs.
// Fall back to the underlying screenshot/capture commands, which will
// still surface a platform error if Screen Recording permission is missing.
func ensureScreenCapturePermission(context.Context) error {
	return nil
}
