//go:build windows

package toolruntime

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

func diskUsage(path string) (total, free, available uint64, err error) {
	volume := filepath.VolumeName(path)
	if volume == "" {
		volume = `C:`
	}
	target := volume + `\`

	ptr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid path %q: %w", target, err)
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")

	var availableBytes uint64
	var totalBytes uint64
	var freeBytes uint64
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&availableBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&freeBytes)),
	)
	if ret == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return 0, 0, 0, callErr
		}
		return 0, 0, 0, fmt.Errorf("GetDiskFreeSpaceExW failed for %q", target)
	}

	return totalBytes, freeBytes, availableBytes, nil
}
