package admin

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type VacuumRunner struct {
	db         *sql.DB
	onProgress func(percent int, bytesReclaimed int64)
}

func NewVacuumRunner(db *sql.DB) *VacuumRunner {
	return &VacuumRunner{db: db}
}

func (v *VacuumRunner) Run(ctx context.Context) (int64, error) {
	preSize, err := v.dbSize(ctx)
	if err != nil {
		return 0, fmt.Errorf("vacuum: pre-size: %w", err)
	}

	done := make(chan struct{})
	var vacuumErr error
	var postSize int64

	go func() {
		_, vacuumErr = v.db.ExecContext(ctx, "VACUUM")
		if vacuumErr == nil {
			postSize, vacuumErr = v.dbSize(ctx)
		}
		close(done)
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-done:
			break loop
		case <-ticker.C:
			if v.onProgress != nil {
				currentSize, err := v.dbSize(ctx)
				if err == nil && preSize > 0 {
					pct := int((preSize - currentSize) * 100 / preSize)
					if pct < 0 {
						pct = 0
					}
					if pct > 99 {
						pct = 99
					}
					v.onProgress(pct, preSize-currentSize)
				}
			}
		}
	}

	if vacuumErr != nil {
		return 0, fmt.Errorf("vacuum: %w", vacuumErr)
	}

	reclaimed := preSize - postSize
	if reclaimed < 0 {
		reclaimed = 0
	}
	if v.onProgress != nil {
		v.onProgress(100, reclaimed)
	}
	return reclaimed, nil
}

func (v *VacuumRunner) dbSize(ctx context.Context) (int64, error) {
	var pageSize, pageCount int64
	if err := v.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	if err := v.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, err
	}
	return pageSize * pageCount, nil
}

// OnProgress sets a callback for progress reporting. Exists so the CLI
// can attach its spinner/progress-bar without modifying the constructor.
func (v *VacuumRunner) OnProgress(fn func(percent int, bytesReclaimed int64)) {
	v.onProgress = fn
}
