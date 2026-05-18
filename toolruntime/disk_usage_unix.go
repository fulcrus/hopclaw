//go:build !windows

package toolruntime

import "syscall"

func diskUsage(path string) (total, free, available uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, err
	}

	blockSize := uint64(stat.Bsize)
	total = stat.Blocks * blockSize
	free = stat.Bfree * blockSize
	available = stat.Bavail * blockSize
	return total, free, available, nil
}
