//go:build windows

package health

import (
	"context"
	"time"
)

// DiskSpaceProbe Windows stub. syscall.Statfs_t / syscall.Statfs are
// POSIX-only — Go has no portable equivalent without golang.org/x/sys/windows
// (intentionally not added as a transitive dep on lint instructions; see
// [Unreleased] → Fixed in docs/CHANGELOG.md). Path arg is ignored: returning
// nil with Severity=warning keeps /health/ready green on Windows operators
// rather than marking the instance degraded for a probe that cannot run.
func DiskSpaceProbe(path string) Check {
	return Check{
		Name: "disk_space",
		Probe: func(ctx context.Context) error {
			return nil
		},
		Timeout:  5 * time.Second,
		Severity: "warning",
	}
}
