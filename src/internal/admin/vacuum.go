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
	preSize, err := v.preflight(ctx)
	if err != nil {
		return 0, err
	}

	postSize, err := v.executeVacuum(ctx, preSize)
	if err != nil {
		return 0, err
	}

	return v.postReport(preSize, postSize), nil
}

func (v *VacuumRunner) preflight(ctx context.Context) (int64, error) {
	size, err := v.dbSize(ctx)
	if err != nil {
		return 0, fmt.Errorf("vacuum: pre-size: %w", err)
	}
	return size, nil
}

func (v *VacuumRunner) executeVacuum(ctx context.Context, preSize int64) (int64, error) {
	done := make(chan struct{})
	var vacuumErr error
	var postSize int64

	go func() {
		_, vacuumErr = v.db.ExecContext(ctx, "VACUUM")
		if vacuumErr == nil {
			postSize, _ = v.dbSize(ctx)
		}
		close(done)
	}()

	if err := v.waitForVacuum(ctx, preSize, done); err != nil {
		return 0, err
	}

	if vacuumErr != nil {
		return 0, fmt.Errorf("vacuum: %w", vacuumErr)
	}
	return postSize, nil
}

func (v *VacuumRunner) waitForVacuum(ctx context.Context, preSize int64, done <-chan struct{}) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		case <-ticker.C:
			v.reportProgress(ctx, preSize)
		}
	}
}

func (v *VacuumRunner) reportProgress(ctx context.Context, preSize int64) {
	if v.onProgress == nil {
		return
	}
	currentSize, err := v.dbSize(ctx)
	if err != nil || preSize <= 0 {
		return
	}
	pct := int((preSize - currentSize) * 100 / preSize)
	if pct < 0 {
		pct = 0
	}
	if pct > 99 {
		pct = 99
	}
	v.onProgress(pct, preSize-currentSize)
}

func (v *VacuumRunner) postReport(preSize, postSize int64) int64 {
	reclaimed := preSize - postSize
	if reclaimed < 0 {
		reclaimed = 0
	}
	if v.onProgress != nil {
		v.onProgress(100, reclaimed)
	}
	return reclaimed
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
