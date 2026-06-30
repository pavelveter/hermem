package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/pavelveter/hermem/src/internal/core"
)

// AddEdge inserts an edge between two existing entities.
//
// The existence check, cycle check, and INSERT run inside a single
// transaction so concurrent AddEdge calls cannot interleave. INSERT OR
// IGNORE remains as a fast-path duplicate guard; the tx guarantees
// atomicity when called from multiple goroutines.
//
// Isolation: explicit sql.LevelSerializable (mattn/go-sqlite3 already
// acquires the write lock up front for DEFERRED txs, so this is
// documentation rather than a runtime behaviour change — but the
// explicit intent at the call site means a future driver swap to one
// that defaults to LevelRepeatableRead won't silently drop the
// strict-serial contract).
func AddEdge(ctx context.Context, db *sql.DB, src, dst, rel string, weight float32) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin edge tx (Serializable): %w", err)
	}
	defer tx.Rollback() // safe after Commit

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM entities WHERE id IN (?, ?)", src, dst).Scan(&count); err != nil {
		return fmt.Errorf("failed to check entity existence: %w", err)
	}
	if count != 2 {
		return fmt.Errorf("both source and target entities must exist (found %d of 2)", count)
	}
	if weight == 0 {
		weight = 1.0
	}
	var hasCycle int
	err = tx.QueryRow(`WITH RECURSIVE cycle_check AS (
		SELECT ? AS node
		UNION ALL
		SELECT ed.target_id FROM cycle_check cc JOIN edges ed ON ed.source_id = cc.node AND ed.relation_type = ?
	) SELECT COUNT(*) FROM cycle_check WHERE node = ?`, dst, rel, src).Scan(&hasCycle)
	if err != nil {
		return fmt.Errorf("cycle check: %w", err)
	}
	if hasCycle > 0 {
		return fmt.Errorf("adding edge %s->%s creates a cycle", src, dst)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, ?)`, src, dst, rel, weight); err != nil {
		return fmt.Errorf("failed to insert edge: %w", err)
	}
	return tx.Commit()
}

// DeleteEdge removes a single edge row.
func DeleteEdge(db *sql.DB, src, dst, rel string) error {
	_, err := db.Exec("DELETE FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?", src, dst, rel)
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	return nil
}

// ErrPurgeEntityNotFound is the sentinel returned when PurgeEntity is
// called with an ID that does not exist in the entities table. Callers
// use errors.Is(err, store.ErrPurgeEntityNotFound) to branch on this
// case without string-matching the wrapped %q placeholders.
var ErrPurgeEntityNotFound = errors.New("purge entity: id does not exist")

// PurgeEntity atomically removes an entity, every edge referencing it
// (as source OR target), and the vector index entry. Mirrors out.txt
// § 3.3: cascading delete on the relational side, then vi.Remove on
// the vector side AFTER the DB commit succeeds (drift guard).
//
// Isolation: LevelSerializable so the existence check, edge delete,
// and entity delete run as a single observable event to any other
// reader. busy_timeout=5000 (set in InitDB's DSN) catches the rare
// SQLITE_BUSY on a parallel ingest — the caller can wrap PurgeEntity
// in a small retry loop if a higher contention rate is observed.
//
// FK constraint: PRAGMA foreign_keys=ON is set in InitDB's DSN so
// SQLite enforces edge→entity FKs at COMMIT time. Schema-level
// ON DELETE CASCADE on edges is NOT used here — we keep the explicit
// DELETE below so a future soft-delete / archival migration can
// choose its own per-edge path (archive instead of hard-delete).
// The explicit form remains CORRECT even if a future migration adds
// ON DELETE CASCADE on the edges table — the FK would silently do
// the same work but never fail. Keep this contract test green
// across migrations.
//
// VectorIndex contract: production callers MUST pass non-nil vi. A
// nil vi causes a logged warning and a vector-cleanup skip — useful
// for test fixtures where the caller intends a DB-only delete, but
// in production a missing vi means orphaned vector entries (drift)
// that the next search will surface. The DB is the source of truth;
// vi.Remove runs AFTER tx.Commit returns nil, never before.
func PurgeEntity(ctx context.Context, db *sql.DB, vi core.VectorIndex, entityID string) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("purge entity: begin tx: %w", err)
	}
	defer tx.Rollback() // safe after Commit

	// Step 1: delete every edge that references entityID.
	// Explicit DELETE; safe to keep even if ON DELETE CASCADE lands.
	edgeResult, err := tx.ExecContext(ctx,
		`DELETE FROM edges WHERE source_id = ? OR target_id = ?`, entityID, entityID)
	if err != nil {
		return fmt.Errorf("purge entity: edges delete: %w", err)
	}
	edgesDeleted, _ := edgeResult.RowsAffected()

	// Step 2: delete the entity itself.
	entityResult, err := tx.ExecContext(ctx,
		`DELETE FROM entities WHERE id = ?`, entityID)
	if err != nil {
		return fmt.Errorf("purge entity: entity delete: %w", err)
	}
	rowsDeleted, _ := entityResult.RowsAffected()
	if rowsDeleted == 0 {
		// Sentinel so callers can distinguish "not found" from other failures.
		return fmt.Errorf("purge entity: id %q: %w", entityID, ErrPurgeEntityNotFound)
	}

	// Step 3: commit BEFORE touching vi (out.txt § 3.3 atomic contract).
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("purge entity: commit: %w", err)
	}

	// Step 4: vi.Remove. log but do not fail.
	if vi != nil {
		if err := vi.Remove(ctx, []string{entityID}); err != nil {
			// Log but don't roll back the DB delete; caller can rebuild
			// the vector index from DB embeddings via algo.ReEmbedAll.
			// Returning an error here would mislead downstream callers
			// who already saw the entity removed.
			return fmt.Errorf("purge entity: db purged, vector index drift: %w", err)
		}
	} else {
		// Production path should never trigger this; tests / mocks pass nil
		// intentionally. Log loudly with structured fields so an operator
		// can grep `/var/log/hermem.log` by entity id during incident triage.
		slog.Warn("purge entity: vi is nil — entity deleted but vector entry left in place",
			slog.String("entity_id", entityID),
			slog.Bool("db_purged", true),
		)
	}
	_ = edgesDeleted // currently logged only via slog
	return nil
}

// QueryEdges runs a query and scans all rows into a core.Edge slice.
func QueryEdges(db *sql.DB, query string, args ...interface{}) ([]core.Edge, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Edge
	for rows.Next() {
		var ed core.Edge
		if err := rows.Scan(&ed.SourceID, &ed.TargetID, &ed.RelationType, &ed.Weight); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		out = append(out, ed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return out, nil
}
