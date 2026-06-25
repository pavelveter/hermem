// Package retention owns the transport-agnostic garbage-collection / archive
// sweep for stale observation entities.
//
// PHASE 3.3 lifts the sweep loop out of src/internal/algo/gc.go (which is
// deleted in this phase) into a flat pkg following the PHASE 2.x + PHASE
// 3.1 + PHASE 3.2 precedent: stateless Service, per-call policy, no HTTP /
// CLI coupling. The HTTP shell lives in src/internal/server/retention/.
//
// Concurrency contract: the long-lived Run loop calls RunOnce on a ticker;
// both methods are safe to invoke concurrently — SQLite's busy_timeout=5000
// + the BEGIN IMMEDIATE writer lock serialize the per-row UPDATE phase
// across sweeps. A partial archive failure ROLLBACKs and skips vi.Remove
// so the ARCH=0 visibility invariant (search filters archived=0) is
// preserved at the cost of holding otherwise-good rows until the next
// ticker. Vector drift is impossible because vi.Remove runs ONLY after a
// successful COMMIT.
package retention

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GCReport is the envelope returned by RunOnce. Simpler than PHASE 3.1's
// ConnectedComponent (no nested lists) because a sweep has flat scalar
// outcomes: a timestamp pair, a single Swept count, and an optional Error.
// The HTTP shell's POST /admin/retention/run handler returns this struct
// verbatim under a `report` key.
type GCReport struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Swept      int       `json:"swept"`
	Error      string    `json:"error,omitempty"`
}

// Service is the transport-agnostic GC sweep orchestrator. Intentionally
// stateless across calls — the long-lived Run loop is a thin Ticker wrapper
// around RunOnce; per-call policy keeps the constructor minimal.
type Service struct {
	db *sql.DB
	vi core.VectorIndex
}

// NewService constructs a retention Service. Both db and vi are required
// (RunOnce removes from vi after every successful archive sweep). The DB's
// MaxOpenConns constraint must be 1 (set by store.InitDB) so the BEGIN
// IMMEDIATE writer-lock serializes against parallel ingest transactions.
func NewService(db *sql.DB, vi core.VectorIndex) *Service {
	return &Service{db: db, vi: vi}
}

// Run polls at policy.RunInterval indefinitely until ctx is cancelled.
// Each sweep delegates to RunOnce so the loop body and the one-shot code
// path stay identical. The HTTP shell's lifecycle manager (see
// src/internal/server/server.go Serve) wires Run into a goroutine that is
// cancelled before close, matching the pre-PHASE-3.3 drain order:
// HTTP → GC cancel → DB close.
func (s *Service) Run(ctx context.Context, policy core.RetentionPolicy) {
	ticker := time.NewTicker(policy.RunInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rep, err := s.RunOnce(ctx, policy)
			if err != nil {
				slog.Error("retention run", "err", err, "report", rep)
				continue
			}
			if rep.Swept > 0 {
				slog.Info("retention archived", "count", rep.Swept)
			}
		}
	}
}

// RunOnce performs a single archive sweep. Safe to call concurrently with
// Run: SQLite's busy_timeout=5000 + the BEGIN IMMEDIATE writer lock
// prevents two sweeps from simultaneously mutating the entities table.
//
// All errors are returned in BOTH the envelope's Error field AND the
// second return value so callers (HTTP shell handles, CLI subcommands)
// can choose whether to log-or-surface without re-parsing the envelope.
//
// Named return values (rep, err): the deferred FinishedAt assignment
// below mutates the named-return copy of the envelope, so callers always
// see the post-deferred timestamp. Returning `rep.X = Y; err = Z; return`
// (rather than `return rep, Z`) keeps the defer effect observable on
// every code path. PHASE 3.3 review-flagged bug: an earlier draft used a
// local GCReport + deferred stamp, which produced a zero FinishedAt on
// every early-return path because the defer mutated the local copy
// AFTER the return value had already been captured by the caller.
func (s *Service) RunOnce(ctx context.Context, policy core.RetentionPolicy) (rep GCReport, err error) {
	rep.StartedAt = time.Now()
	defer func() {
		rep.FinishedAt = time.Now()
	}()

	cutoff := time.Now().Add(-policy.ObservationTTL)
	rows, qerr := s.db.QueryContext(ctx,
		`SELECT id FROM entities WHERE category = 'observation' AND updated_at < ? AND archived = 0 LIMIT ?`,
		cutoff, policy.DeleteBatchSize)
	if qerr != nil {
		rep.Error = qerr.Error()
		err = fmt.Errorf("retention: select: %w", qerr)
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			rep.Error = serr.Error()
			err = fmt.Errorf("retention: scan: %w", serr)
			return
		}
		ids = append(ids, id)
	}
	if rerr := rows.Err(); rerr != nil {
		rep.Error = rerr.Error()
		err = fmt.Errorf("retention: rows: %w", rerr)
		return
	}
	if len(ids) == 0 {
		rep.Swept = 0
		return
	}

	// Defensive ROLLBACK before BEGIN IMMEDIATE: a previous sweep that
	// errored mid-flight might have left the single MaxOpenConns=1
	// connection inside an open transaction. SQLite documents `cannot
	// start a transaction within a transaction` if we try to BEGIN
	// IMMEDIATE in that state. Issuing a no-op ROLLBACK first clears
	// the broken state cheaply. Mirrors the pre-extraction algo/gc.go
	// comment § 3.4 verbatim.
	_, _ = s.db.ExecContext(ctx, "ROLLBACK") //nolint:errcheck // defensive: clears broken state on conn reuse; err from ROLLBACK on a non-tx conn is benign
	if berr := beginImmediate(ctx, s.db); berr != nil {
		rep.Error = berr.Error()
		err = fmt.Errorf("retention: begin immediate: %w", berr)
		return
	}

	// Per-row archive: a partial UPDATE failure ROLLBACKs and skips
	// vi.Remove so the ARCH=0 visibility invariant (search filters
	// archived=0) is preserved at the cost of holding otherwise-good
	// rows until the next ticker. We choose ROLLBACK over per-row
	// retry because the COMMIT-or-ROLLBACK binary decision is
	// trivially auditable; per-row retry would surface partial
	// archive state to readers. Drift from held-back rows is bounded
	// by RunInterval — not catastrophic.
	var errorOccurred bool
	for _, id := range ids {
		_, uerr := s.db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, id) //nolint:errcheck // Result (sql.Result) discarded; err captured into uerr below
		if uerr != nil {
			slog.Warn("retention archive", "id", id, "err", uerr)
			errorOccurred = true
		}
	}
	if errorOccurred {
		rep.Error = "partial archive failure"
		_ = rollbackCurrentTx(ctx, s.db) //nolint:errcheck // defensive: clears broken connection state, err is benign since call site has the BEGIN IMMEDIATE guard
		err = fmt.Errorf("retention: partial archive failure")
		return
	}
	if cerr := commitCurrentTx(ctx, s.db); cerr != nil {
		rep.Error = cerr.Error()
		_ = rollbackCurrentTx(ctx, s.db) //nolint:errcheck // defensive: clears broken connection state, err is benign since call site has the BEGIN IMMEDIATE guard
		err = fmt.Errorf("retention: commit: %w", cerr)
		return
	}

	// vi.Remove only AFTER successful COMMIT. If the COMMIT fails the
	// row stays un-archived and is eligible again next sweep; vi wasn't
	// touched, so no vector drift from a failed commit. vi.Remove itself
	// may still fail at runtime (e.g. memory pool exhaustion); we
	// capture that into `verr` and log it, but we do NOT fail the sweep
	// because the DB state is already committed. Ghost vectors from a
	// failed removal persist until manual cleanup (no auto GC).
	if verr := s.vi.Remove(ctx, ids); verr != nil {
		slog.Warn("retention: vi.Remove post-commit fault", "count", len(ids), "err", verr)
	}
	rep.Swept = len(ids)
	return
}

// beginImmediate opens a writer-locked transaction via SQL `BEGIN IMMEDIATE`.
// Hand-rolled against *sql.DB because the project's mattn driver uses
// DEFERRED transactions on a connection. SQLite busy_timeout=5000 falls
// back gracefully when BEGIN IMMEDIATE blocks on a held writer lock.
// Lives as a private helper inside retention pkg to avoid a circular
// import with the deleted algo pkg.
func beginImmediate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "BEGIN IMMEDIATE")
	return err
}

// commitCurrentTx + rollbackCurrentTx — paired SQL wrappers because the
// sweep uses BEGIN IMMEDIATE / COMMIT / ROLLBACK directly on the conn
// instead of sql.Tx. Mismatched call pairs would leave the conn in an
// open transaction blocking every subsequent writer, so both helpers
// stay symmetric and log on rollback.
func commitCurrentTx(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "COMMIT")
	return err
}

func rollbackCurrentTx(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "ROLLBACK")
	if err != nil {
		slog.Warn("retention rollback", "err", err)
	}
	return err
}
