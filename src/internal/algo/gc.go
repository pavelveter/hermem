package algo

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GarbageCollector periodically archives stale observation nodes per policy.
//
// Concurrency contract: the loop runs once per RunInterval. Each sweep opens
// at most one transaction at a time; concurrent writers (parallel ingest
// goroutines, retention restarts) fall through to PRAGMA busy_timeout=5000
// via store.InitDB and resolve on the SQLite busy-latch. BEGIN IMMEDIATE
// would still reduce SQLITE_BUSY retries by acquiring the writer lock
// up-front, but Go-SQLite's mattn driver already does this implicitly
// for DEFERRED transactions on the same connection — see the comment on
// `beginImmediate` below for the case where it's truly needed.
//
// VectorIndex contract: vi.Remove runs ONLY after a successful DB commit.
// If the commit fails the row stays un-archived and is eligible again
// next sweep; no vector drift is possible because vi wasn't touched.
//
// Single-row-archive policy: a partial UPDATE failure aborts the entire
// sweep via ROLLBACK + vi.Remove skip, preserving the ARCH=0 visibility
// invariant (search filters archived=0 entries) at the cost of holding
// otherwise-good rows until the next ticker. We choose ROLLBACK over
// per-row retry because the COMMIT-or-ROLLBACK binary decision is
// trivially auditable; a per-row retry mode would surface partial
// archive state to readers. Drift accruing from the held-back rows is
// bounded by RunInterval — not catastrophic.
func GarbageCollector(ctx context.Context, db *sql.DB, vi core.VectorIndex, policy core.RetentionPolicy) {
	ticker := time.NewTicker(policy.RunInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-policy.ObservationTTL)
			rows, err := db.QueryContext(ctx, `SELECT id FROM entities WHERE category = 'observation' AND updated_at < ? AND archived = 0 LIMIT ?`, cutoff, policy.DeleteBatchSize)
			if err != nil {
				slog.Error("gc query", "err", err)
				continue
			}
			var ids []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				ids = append(ids, id)
			}
			rows.Close()
			if len(ids) == 0 {
				continue
			}
			// FIX § 3.4 (out.txt): upgrade to BEGIN IMMEDIATE so the writer
			// lock is acquired at sweep entry, eliminating the SQLITE_BUSY
			// retry window where a parallel ingest tx could win the lock
			// after CollectDeadNodes started. Implementation: mattn/go-sqlite3
			// honours the `_txlock` URI parameter or, equivalently, the
			// `BEGIN IMMEDIATE` SQL statement. We use the SQL form because
			// it works on the existing DSN without re-opening the pool.
			//
			// Defensive ROLLBACK before each BEGIN IMMEDIATE: a previous
			// sweep that errored mid-flight might have left the (single)
			// connection inside an open transaction. SQLite documents
			// `cannot start a transaction within a transaction` if we
			// try to BEGIN IMMEDIATE in that state. Issuing a no-op
			// ROLLBACK first clears the broken state cheaply.
			_, _ = db.ExecContext(ctx, "ROLLBACK") // best-effort; harmless if no open tx
			if err := beginImmediate(ctx, db); err != nil {
				slog.Warn("gc begin immediate", "err", err)
				continue
			}
			// Track per-row failure to keep the COMMIT decision atomic:
			// a partial UPDATE failure earlier in the loop must NOT push
			// vi.Remove unconditionally, because that would orphan vector
			// entries for entities whose row is still archived=0.
			var errorOccurred bool
			for _, id := range ids {
				if _, err := db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, id); err != nil {
					slog.Warn("gc archive", "id", id, "err", err)
					errorOccurred = true
				}
			}
			if errorOccurred {
				slog.Warn("gc archive: partial failure, rolling back without vi.Remove")
				_ = rollbackCurrentTx(ctx, db)
				continue
			}
			if err := commitCurrentTx(ctx, db); err != nil {
				slog.Warn("gc commit", "err", err)
				_ = rollbackCurrentTx(ctx, db)
				continue // vi.Remove only after successful commit
			}
			vi.Remove(ctx, ids)
			slog.Info("gc archived", "count", len(ids))
		}
	}
}

// beginImmediate opens a writer-locked transaction via SQL `BEGIN IMMEDIATE`.
// Implemented against *sql.DB so the call site can stay simple: the existing
// mattn/sqlite3 driver issues BEGIN IMMEDIATE on a DEFERRED tx path when the
// first write statement would otherwise block, but doing it explicitly here
// removes the SQLITE_BUSY retry window between the initial SELECT and the
// first UPDATE.
//
// The helper is a thin SQL exec wrapper rather than `sql.TxOptions{}`-based
// BeginTx because the project uses Connection pooling=1 (`store.InitDB` sets
// `MaxOpenConns=1`); a `LevelSerializable` BeginTx would still go DEFERRED on
// this driver. Hand-rolling BEGIN IMMEDIATE matches out.txt § 3.4 verbatim.
func beginImmediate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "BEGIN IMMEDIATE")
	return err
}

// commitCurrentTx + rollbackCurrentTx — paired helpers because the GC sweep
// uses BEGIN IMMEDIATE/COMMIT directly on the conn instead of Tx. Mismatched
// call pairs would leave the conn in an open transaction blocking every
// subsequent writer, so keep both helpers symmetric and log on rollback.
func commitCurrentTx(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "COMMIT")
	return err
}

func rollbackCurrentTx(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "ROLLBACK")
	if err != nil {
		slog.Warn("gc rollback", "err", err)
	}
	return err
}
