//go:build !windows

package health

import (
	"context"
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

func DiskSpaceProbe(path string) Check {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	return Check{
		Name: "disk_space",
		Probe: func(ctx context.Context) error {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(dir, &stat); err != nil {
				return fmt.Errorf("statfs: %w", err)
			}
			freeBytes := stat.Bavail * uint64(stat.Bsize)
			const minFree = 100 * 1024 * 1024
			if freeBytes < minFree {
				return fmt.Errorf("low disk space: %d bytes free, need %d", freeBytes, minFree)
			}
			return nil
		},
		Timeout:  5 * time.Second,
		Severity: "critical",
	}
}
