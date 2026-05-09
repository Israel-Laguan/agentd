package safety

import (
	"fmt"
	"syscall"
)

// DiskFreePercent returns the percentage of free blocks for the filesystem
// containing path. Uses syscall.Statfs which limits portability to Unix-like
// hosts; a platform-specific helper would be needed for Windows support.
func DiskFreePercent(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	if stat.Blocks == 0 {
		return 0, fmt.Errorf("statfs %s: zero total blocks", path)
	}
	return (float64(stat.Bavail) / float64(stat.Blocks)) * 100, nil
}
