package retention

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// ConfidenceLifecycleConfig controls the confidence-based cleanup behavior.
type ConfidenceLifecycleConfig struct {
	// Enabled gates the entire lifecycle. Default false (disabled).
	Enabled bool
	// ConfidenceThreshold entities below this confidence are "candidate" knowledge.
	ConfidenceThreshold float32
	// TTL is how long a low-confidence entity survives without reinforcement.
	TTL time.Duration
	// RunInterval is how often the sweep runs.
	RunInterval time.Duration
	// BatchSize limits entities per sweep cycle.
	BatchSize int
}

// DefaultConfidenceLifecycleConfig returns production defaults (disabled).
func DefaultConfidenceLifecycleConfig() ConfidenceLifecycleConfig {
	return ConfidenceLifecycleConfig{
		Enabled:             false,
		ConfidenceThreshold: 0.7,
		TTL:                 30 * 24 * time.Hour, // 30 days
		RunInterval:         1 * time.Hour,
		BatchSize:           200,
	}
}

// ConfidenceLifecycleReport is the result of a single sweep.
type ConfidenceLifecycleReport struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Archived   int       `json:"archived"`
	Error      string    `json:"error,omitempty"`
}

// ConfidenceLifecycle manages the TTL-based expiry of low-confidence
// ("candidate") knowledge. Entities with confidence below the threshold
// that haven't been updated within the TTL are archived.
//
// This service is optional — some deployments require immutable knowledge.
// Set Enabled=true in config to activate.
type ConfidenceLifecycle struct {
	db *sql.DB
}

// NewConfidenceLifecycle constructs a ConfidenceLifecycle.
func NewConfidenceLifecycle(db *sql.DB) *ConfidenceLifecycle {
	return &ConfidenceLifecycle{db: db}
}

// Run polls at cfg.RunInterval until ctx is cancelled.
func (cl *ConfidenceLifecycle) Run(ctx context.Context, cfg ConfidenceLifecycleConfig) {
	if !cfg.Enabled {
		slog.Info("confidence lifecycle: disabled")
		return
	}
	ticker := time.NewTicker(cfg.RunInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rep, err := cl.RunOnce(ctx, cfg)
			if err != nil {
				slog.Error("confidence lifecycle run", "err", err, "report", rep)
				continue
			}
			if rep.Archived > 0 {
				slog.Info("confidence lifecycle archived", "count", rep.Archived)
			}
		}
	}
}

// RunOnce performs a single sweep of low-confidence expired entities.
func (cl *ConfidenceLifecycle) RunOnce(ctx context.Context, cfg ConfidenceLifecycleConfig) (rep ConfidenceLifecycleReport, err error) {
	rep.StartedAt = time.Now()
	defer func() { rep.FinishedAt = time.Now() }()

	if !cfg.Enabled {
		return
	}

	threshold := cfg.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.7
	}
	cutoff := time.Now().Add(-cfg.TTL)
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 200
	}

	// Find low-confidence entities that haven't been updated within TTL.
	rows, qerr := cl.db.QueryContext(ctx,
		`SELECT id FROM entities
		 WHERE confidence < ? AND confidence > 0
		   AND updated_at < ? AND archived = 0
		 LIMIT ?`,
		threshold, cutoff, batchSize)
	if qerr != nil {
		rep.Error = qerr.Error()
		err = fmt.Errorf("confidence lifecycle: select: %w", qerr)
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			rep.Error = serr.Error()
			err = fmt.Errorf("confidence lifecycle: scan: %w", serr)
			return
		}
		ids = append(ids, id)
	}
	if rerr := rows.Err(); rerr != nil {
		rep.Error = rerr.Error()
		err = fmt.Errorf("confidence lifecycle: rows: %w", rerr)
		return
	}
	if len(ids) == 0 {
		return
	}

	// Archive expired low-confidence entities.
	_, _ = cl.db.ExecContext(ctx, "ROLLBACK")
	if berr := beginImmediate(ctx, cl.db); berr != nil {
		rep.Error = berr.Error()
		err = fmt.Errorf("confidence lifecycle: begin immediate: %w", berr)
		return
	}

	for _, id := range ids {
		slog.Debug("confidence lifecycle: archiving", "id", id)
		if _, uerr := cl.db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, id); uerr != nil {
			slog.Warn("confidence lifecycle archive", "id", id, "err", uerr)
			rep.Error = "partial archive failure"
			_ = rollbackCurrentTx(ctx, cl.db)
			err = fmt.Errorf("confidence lifecycle: partial archive failure")
			return
		}
	}

	if cerr := commitCurrentTx(ctx, cl.db); cerr != nil {
		rep.Error = cerr.Error()
		_ = rollbackCurrentTx(ctx, cl.db)
		err = fmt.Errorf("confidence lifecycle: commit: %w", cerr)
		return
	}

	rep.Archived = len(ids)
	slog.Info("confidence lifecycle sweep completed", "archived", len(ids), "threshold", threshold, "ttl", cfg.TTL)
	return
}
