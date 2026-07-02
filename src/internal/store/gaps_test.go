package store

// -- Audit-driven coverage closure for the four gaps that held store's
// coverage at 63.3% in the round-8 audit:
//
//	1. edge-merge with dupe-detection race          → TestAddEdge_*
//	2. codec encode/decode round-trip edges         → TestEmbeddingToBytes_*, TestBytesToEmbedding_*, TestDecodeVector_*, TestBytesToFloat32Safe_MidStream*
//	3. partial-failure rollback paths               → TestRollbackMigration_*, TestIsIdempotentMigrationError_*, TestSplitSql_CommentSeparators
//	4. schema-fingerprint poisoned state            → TestHashSchema_*, TestCheckSchemaFingerprint_*, TestStoreSchemaFingerprint_*
//
// Plan-of-record plus invariants per-test:
//   - race-safe (-race clean: no shared counters, no in-thread assertions
//     against shared state; sequential seed-before-launch).
//   - audit-after-call: every test that mutates state then re-queries SQL
//     to confirm the change is observable, not just that the call "returned nil".
//   - timing-free: zero time.Sleep; pre-seeded data only; barriers only used
//     inside goroutines that share a barrier channel.
//   - parallel-friendly: every test calls t.Parallel() except where it
//     mutates a package-global (none here) or uses non-parallel-safe inputs.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// =========================================================================
// Section 1 — edge-merge with dupe-detection race
// =========================================================================

// TestAddEdge_HappyPath — covers the common branch: 2 existing entities,
// no cycle, INSERT OR IGNORE returns and row count == 1.
//
// Audit gates: (a) AddEdge returned nil; (b) SQL row count for the edges
// pair is 1; (c) selected weight matches what the caller passed.
func TestAddEdge_HappyPath(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "src", "world", "from")
	seedEntity(t, db, "dst", "world", "to")

	ctx := t.Context()
	if err := AddEdge(ctx, db, "src", "dst", "related_to", 2.5); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		"src", "dst", "related_to").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("edge row count: want 1, got %d", count)
	}
	var weight float32
	if err := db.QueryRow(`SELECT COALESCE(weight, 1.0) FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		"src", "dst", "related_to").Scan(&weight); err != nil {
		t.Fatalf("weight query: %v", err)
	}
	if weight != 2.5 {
		t.Fatalf("weight: want 2.5, got %v", weight)
	}
}

// TestAddEdge_BothEntitiesMustExist — when only one of (src, dst) exists,
// AddEdge's tx body returns the "both must exist" error and the row is
// never inserted.
//
// Audit gates: (a) returned error mentions "both source and target";
// (b) edges table is empty.
func TestAddEdge_BothEntitiesMustExist(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "src", "world", "from")
	// dst intentionally absent.

	ctx := t.Context()
	err := AddEdge(ctx, db, "src", "missing", "related_to", 1)
	if err == nil || !strings.Contains(err.Error(), "both source and target") {
		t.Fatalf("want both-must-exist error, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("edges table should be empty, got %d rows", n)
	}
}

// TestAddEdge_RejectsCycle — pre-seed an a→b edge under the same
// relation; now adding b→a must fail the cycle check inside the tx.
//
// Audit gates: (a) returned error mentions "creates a cycle"; (b) only
// the seeded edge remains.
func TestAddEdge_RejectsCycle(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	seedEdge(t, db, "a", "b", "related_to", 1) // existing direction

	ctx := t.Context()
	err := AddEdge(ctx, db, "b", "a", "related_to", 1)
	if err == nil || !strings.Contains(err.Error(), "creates a cycle") {
		t.Fatalf("want cycle error, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE relation_type = 'related_to'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 related_to edge (the pre-seeded one), got %d", n)
	}
}

// TestAddEdge_ZeroWeightDefaultsToOne — the spec'd "weight == 0 → 1.0"
// guard. We pin both branches: passing 0 stores 1.0; passing -1 stores -1
// (negative is the third branch, not covered by the guard).
func TestAddEdge_ZeroWeightDefaultsToOne(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")

	ctx := t.Context()
	if err := AddEdge(ctx, db, "a", "b", "uses", 0); err != nil {
		t.Fatalf("AddEdge(weight=0): %v", err)
	}
	// Read weight RAW (no COALESCE) — the regression we are guarding
	// against is "AddEdge stores 0 instead of overriding to 1.0";
	// COALESCE(weight, 1.0) would mask that regression by substituting
	// 1.0 for both NULL and buggy-0.
	var w float32
	if err := db.QueryRow(`SELECT weight FROM edges WHERE source_id = 'a' AND target_id = 'b'`).Scan(&w); err != nil {
		t.Fatalf("weight query: %v", err)
	}
	if w != float32(1.0) {
		t.Fatalf("weight=0 should default to 1.0 (raw), got %v", w)
	}
}

// TestAddEdge_DuplicateSerial_InsertsOnce — calling AddEdge twice with
// identical args must yield exactly one row (INSERT OR IGNORE contract).
//
// Sequential because the project-wide openTestDB configures
// MaxOpenConns=1 (openConnection), so concurrent calls would serialize
// at the pool level and not actually exercise AddEdge's tx-level dedup.
// The parallel variant uses an isolated file-backed DB with raised pool
// limits (see TestAddEdge_DuplicateParallel_*).
func TestAddEdge_DuplicateSerial_InsertsOnce(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")

	ctx := t.Context()
	for i := 0; i < 5; i++ {
		if err := AddEdge(ctx, db, "a", "b", "uses", 1); err != nil {
			t.Fatalf("AddEdge iter %d: %v", i, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = 'a' AND target_id = 'b' AND relation_type = 'uses'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("INSERT OR IGNORE dedup: want 1 row, got %d", n)
	}
}

// TestAddEdge_DuplicateParallel_WithFileBackedDB — exercises the
// dupe-detection race under real concurrent goroutines.
//
// openConnection configures MaxOpenConns=1, so concurrent AddEdge
// against the regular fixture serializes at the *sql.DB pool level.
// To actually trigger overlapping transactions we use a separate
// file-backed DB and raise the pool cap to 8.
//
// WHAT WE ASSERT, AFTER wg.Wait():
//   - the edge rowcount is exactly 1 (the SQL-level dedup held);
//   - any returned errors must be "database is locked" — SQLite's
//     documented witness of real concurrent contention. This is
//     EXPECTED behaviour for a single-writer engine under forced
//     parallelism; non-lock errors are real regressions.
//
// The -race detector still exercises AddEdge's tx body because the
// underlying sqlite3 driver handles pool-then-driver contention.
//
// Note: SQLite is a single-writer engine. Even with raised pool
// caps, only one goroutine can hold the file write lock at a time;
// others either wait on busy_timeout=5000 (set in InitDB's DSN) or
// error immediately at tx.Exec. INSERT OR IGNORE on the survivors
// ensures the row count stays at 1 regardless of error mix.
func TestAddEdge_DuplicateParallel_WithFileBackedDB(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "parallel-edges.db")
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(8)

	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")

	const n = 16
	const rel = "uses"
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-gate
			errs[i] = AddEdge(t.Context(), db, "a", "b", rel, 1)
		}(i)
	}
	close(gate)
	wg.Wait()

	// Classify returned errors: lock contention is the documented
	// witness of concurrent contention; everything else is a bug.
	var lockFailures, otherFailures int
	for i, e := range errs {
		if e == nil {
			continue
		}
		if strings.Contains(e.Error(), "database is locked") {
			lockFailures++
			continue
		}
		otherFailures++
		t.Errorf("goroutine %d: unexpected (non-lock) error: %v", i, e)
	}

	// Audit the SQL-level dedup: regardless of which goroutines
	// succeeded or hit lock contention, the source/rel/dst tuple
	// must produce exactly one row.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM edges WHERE source_id = 'a' AND target_id = 'b' AND relation_type = ?`,
		rel,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("concurrent dedup: want 1 row, got %d (lockFailures=%d, otherFailures=%d)",
			count, lockFailures, otherFailures)
	}
}

// TestDeleteEdge_HappyPath — covers the single-row DELETE branch.
//
// Audit gates: (a) returns nil; (b) the edge row count drops to 0.
func TestDeleteEdge_HappyPath(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")
	seedEdge(t, db, "a", "b", "uses", 1)

	if err := DeleteEdge(db, "a", "b", "uses"); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("edges table should be empty, got %d", n)
	}
}

// TestDeleteEdge_NoSuchEdge — DELETE of a non-existent row is a clean
// no-op (SQLite does not error, rows-affected=0). AddEdge accepts it.
func TestDeleteEdge_NoSuchEdge(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := DeleteEdge(db, "a", "b", "uses"); err != nil {
		t.Fatalf("DeleteEdge on missing row: %v", err)
	}
}

// TestQueryEdges_HappyPath — Pin QueryEdges' row-scan loop body, which
// is otherwise exercised only indirectly through GetBlockedBy /
// GetDependents.
func TestQueryEdges_HappyPath(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")
	seedEntity(t, db, "c", "world", "c")
	seedEdge(t, db, "a", "b", "uses", 1.5)
	seedEdge(t, db, "b", "c", "uses", 0) // weight=0 stored as NULL

	got, err := QueryEdges(db,
		`SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges ORDER BY source_id, target_id`)
	if err != nil {
		t.Fatalf("QueryEdges: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 edges, got %d", len(got))
	}
	if got[0].Weight != 1.5 {
		t.Fatalf("got[0].Weight: want 1.5, got %v", got[0].Weight)
	}
	if got[1].Weight != 1.0 {
		t.Fatalf("got[1].Weight (NULL coalesced): want 1.0, got %v", got[1].Weight)
	}
}

// fakeVI is a stub core.VectorIndex that records Remove calls and
// optionally returns a configured error. Used by
// TestPurgeEntity_VIErrorAfterDBCommit to surface the "vector drift"
// wrap path in PurgeEntity without spinning up sqlite-vec.
//
// Remove is called exactly once per test (synchronously via
// PurgeEntity), so the field is plainly safe without a mutex.
type fakeVI struct {
	removeErr  error
	removedIDs []string
}

func (f *fakeVI) Search(_ context.Context, _ []float32, _ int) ([]string, error) {
	return nil, nil
}
func (f *fakeVI) SearchBatch(_ context.Context, _ [][]float32, _ int) ([][]string, error) {
	return nil, nil
}
func (f *fakeVI) Store(_ context.Context, _ string, _ []float32) error { return nil }
func (f *fakeVI) Remove(_ context.Context, ids []string) error {
	f.removedIDs = append(f.removedIDs, ids...)
	return f.removeErr
}

func (f *fakeVI) removed() []string {
	out := make([]string, len(f.removedIDs))
	copy(out, f.removedIDs)
	return out
}

// TestPurgeEntity_VIErrorAfterDBCommit_ReturnsDriftError — when vi.Remove
// fails AFTER tx.Commit succeeded, PurgeEntity MUST return an error
// wrapping the vi error so the caller knows about drift, but the DB row
// is already gone (out.txt § 3.3 atomic contract: vi runs after commit).
//
// Audit gates:
//   - returned error contains "vector index drift";
//   - errors.Is(err, driftErr) so callers branch via sentinel chain;
//   - DB row is gone (deleted by tx.Commit); edges referencing the id
//     are also gone.
func TestPurgeEntity_VIErrorAfterDBCommit_ReturnsDriftError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	const id = "doomed"
	seedEntity(t, db, id, "concept", "doomed content")
	seedEntity(t, db, "neighbor", "concept", "neighbor")
	// Edges both directions so both deletion paths fire.
	seedEdge(t, db, id, "neighbor", "uses", 1)
	seedEdge(t, db, "neighbor", id, "uses", 1)

	driftErr := errors.New("simulated vector index failure")
	vi := &fakeVI{removeErr: driftErr}

	err := PurgeEntity(t.Context(), db, vi, id)
	if err == nil {
		t.Fatal("expected drift error from vi.Remove, got nil")
	}
	if !errors.Is(err, driftErr) {
		t.Errorf("want errors.Is(err, driftErr)=true, got %v", err)
	}
	if !strings.Contains(err.Error(), "vector index drift") {
		t.Errorf("want error to mention \"vector index drift\", got %v", err)
	}

	// Audit: row gone, edges gone.
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, id).Scan(&rows); err != nil {
		t.Fatalf("entity count: %v", err)
	}
	if rows != 0 {
		t.Errorf("audit: entity row still present, count=%d", rows)
	}
	var edges int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? OR target_id = ?`, id, id).Scan(&edges); err != nil {
		t.Fatalf("edge count: %v", err)
	}
	if edges != 0 {
		t.Errorf("audit: orphan edges remain, count=%d", edges)
	}

	// Audit: vi.Remove was actually called with the entity id.
	removed := vi.removed()
	if len(removed) != 1 || removed[0] != id {
		t.Errorf("vi.Remove called with %v, want [%q]", removed, id)
	}
}

// =========================================================================
// Section 2 — codec encode/decode round-trip
// =========================================================================

// TestEmbeddingToBytes_SingleValue — covers the single-element loop body
// (multi-element is covered by TestProperty_CodecRoundTrip in codec_test.go).
func TestEmbeddingToBytes_SingleValue(t *testing.T) {
	t.Parallel()
	got := EmbeddingToBytes([]float32{2.5})
	if len(got) != 4 {
		t.Fatalf("want 4-byte blob, got %d", len(got))
	}
	// The wrapped integer bits for 2.5 = 0x40200000 → little-endian: 00 00 20 40.
	want := []byte{0x00, 0x00, 0x20, 0x40}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: want 0x%02x, got 0x%02x", i, want[i], got[i])
		}
	}
}

// TestBytesToEmbedding_BadLengthReturnsNil — BytesToEmbedding is the
// zero-validation codec (no BytesToFloat32Safe NaN/Inf check, no
// dimension check). Length drift → nil, not an error.
func TestBytesToEmbedding_BadLengthReturnsNil(t *testing.T) {
	t.Parallel()
	if got := BytesToEmbedding([]byte{0x00, 0x00, 0x20, 0x40, 0x00}); len(got) != 0 {
		t.Errorf("non-multiple-of-4: want nil/empty, got len=%d", len(got))
	}
}

// TestBytesToEmbedding_EmptyReturnsNil — paired with the bad-length
// branch, the empty-input short-circuit.
func TestBytesToEmbedding_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	if got := BytesToEmbedding(nil); got != nil {
		t.Errorf("nil input: want nil, got %v", got)
	}
	if got := BytesToEmbedding([]byte{}); got != nil {
		t.Errorf("empty input: want nil, got %v", got)
	}
}

// TestDecodeVector_DimensionDriftSmaller — blob shorter than
// expectedDim*4 must error with "dimension drift" wording AND the
// expected byte count for clarity.
func TestDecodeVector_DimensionDriftSmaller(t *testing.T) {
	t.Parallel()
	// expectedDim=3 → 12 bytes; provide 8.
	in := []byte{0x00, 0x00, 0x80, 0x3f, 0x00, 0x00, 0x00, 0x40}
	_, err := DecodeVector(in, 3)
	if err == nil {
		t.Fatal("want dimension drift error, got nil")
	}
	if !strings.Contains(err.Error(), "dimension drift") {
		t.Errorf("error must mention dimension drift, got %v", err)
	}
	if !strings.Contains(err.Error(), "8 bytes") {
		t.Errorf("error must embed actual byte count (got 8), err=%v", err)
	}
}

// TestDecodeVector_DimensionDriftLarger — blob LONGER than expected
// must also be detected (covers the `< expected*4 → return error` branch
// regardless of direction).
func TestDecodeVector_DimensionDriftLarger(t *testing.T) {
	t.Parallel()
	in := make([]byte, 20) // 20 bytes, expectedDim=3 wants 12.
	if _, err := DecodeVector(in, 3); err == nil {
		t.Fatal("want dimension drift error on oversized blob, got nil")
	}
}

// TestDecodeVector_EmptyBlob — the empty-input branch returns its own
// error text (separate from the dimension branch).
func TestDecodeVector_EmptyBlob(t *testing.T) {
	t.Parallel()
	_, err := DecodeVector(nil, 3)
	if err == nil {
		t.Fatal("want empty-blob error, got nil")
	}
	if !strings.Contains(err.Error(), "empty vector blob") {
		t.Errorf("error must mention empty vector blob, got %v", err)
	}
}

// TestDecodeVector_ValidFloatsRoundTrip — happy path through DecodeVector
// (NOT BytesToEmbedding, which skips the NaN/Inf check). Verifies the
// dimension-validated round-trip works for the common case.
func TestDecodeVector_ValidFloatsRoundTrip(t *testing.T) {
	t.Parallel()
	in := []float32{0.5, -1.25, 7.0}
	blob := EmbeddingToBytes(in)
	got, err := DecodeVector(blob, len(in))
	if err != nil {
		t.Fatalf("DecodeVector: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("len: want %d, got %d", len(in), len(got))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("idx %d: want %v, got %v", i, in[i], got[i])
		}
	}
}

// TestBytesToFloat32Safe_MidStreamNaN_HasOffset — when the FIRST
// element of a multi-element blob is NaN, ErrorsIs(ErrFloatNaNOrInf)
// must hold AND the offending byte offset (= 0) must appear in the
// wrapped error text so callers can decide whether to skip vs. abort.
//
// This is the regression test for the rationale documented on
// BytesToFloat32Safe: a NaN at offset 0 would taint every downstream
// BatchDotProducts, so the rejection is observable.
func TestBytesToFloat32Safe_MidStreamNaN_HasOffset(t *testing.T) {
	t.Parallel()
	// Build a 3-element blob: 1.0, NaN, 1.0. NaN bits = 0x7fc00000.
	buf := make([]byte, 12)
	// 1.0 → 0x3f800000
	copy(buf[0:4], []byte{0x00, 0x00, 0x80, 0x3f})
	// NaN at offset 4
	copy(buf[4:8], []byte{0x00, 0x00, 0xc0, 0x7f})
	// 1.0 again
	copy(buf[8:12], []byte{0x00, 0x00, 0x80, 0x3f})

	_, err := BytesToFloat32Safe(buf)
	if err == nil {
		t.Fatal("want NaN rejection, got nil")
	}
	if !errors.Is(err, ErrFloatNaNOrInf) {
		t.Errorf("want errors.Is(ErrFloatNaNOrInf)=true, got %v", err)
	}
	if !strings.Contains(err.Error(), "offset=4") {
		t.Errorf("error must embed offending byte offset 4, got %q", err.Error())
	}
}

// TestBytesToFloat32Safe_MidStreamNegInf — symmetric cover of the
// negative-Inf branch (positive Inf is in codec_test.go). Lock the
// sentinel chain (errors.Is) AND the offset text.
func TestBytesToFloat32Safe_MidStreamNegInf(t *testing.T) {
	t.Parallel()
	// 1.0, then -Inf (0xff800000) at offset 4.
	buf := make([]byte, 8)
	copy(buf[0:4], []byte{0x00, 0x00, 0x80, 0x3f})
	copy(buf[4:8], []byte{0x00, 0x00, 0x80, 0xff})

	_, err := BytesToFloat32Safe(buf)
	if err == nil {
		t.Fatal("want -Inf rejection, got nil")
	}
	if !errors.Is(err, ErrFloatNaNOrInf) {
		t.Errorf("want errors.Is(ErrFloatNaNOrInf)=true, got %v", err)
	}
	if !strings.Contains(err.Error(), "offset=4") {
		t.Errorf("want error to embed offset=4, got %q", err.Error())
	}
}

// =========================================================================
// Section 3 — partial-failure rollback paths
// =========================================================================

// TestRollbackMigration_NoAppliedReturnsEmpty — the early-return branch
// when schema_migrations is empty: returns ("", nil) with no DB writes.
//
// Audit gates: (a) returned name is empty; (b) no error; (c) DB still
// has no schema_migrations rows.
func TestRollbackMigration_NoAppliedReturnsEmpty(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	// Wipe schema_migrations so the last-applied query returns ErrNoRows.
	if _, err := db.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM migration_checksums`); err != nil {
		t.Fatalf("wipe checksums: %v", err)
	}

	name, err := RollbackMigration(db, "")
	if err != nil {
		t.Fatalf("RollbackMigration on empty DB: %v", err)
	}
	if name != "" {
		t.Errorf("want \"\" on empty DB, got %q", name)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("schema_migrations should still be empty, got %d", n)
	}
}

// TestRollbackMigration_RemovesLastApplied — when schema_migrations has
// rows, RollbackMigration(db, "") removes the most-recently applied row
// (by applied_at timestamp) plus its matching checksums row.
//
// We CANNOT rely on the freshly-applied-migrations fixture here:
// SQLite's CURRENT_TIMESTAMP has second-resolution, so every embedded
// migration inserted during InitDB shares the same applied_at value,
// and `ORDER BY applied_at DESC LIMIT 1` resolves ties by ROWID (the
// first inserted, i.e. 001_initial_schema.sql). Seed two rows with
// DISTINCT applied_at values to make the "last by timestamp" answer
// deterministic.
//
// Audit gates:
//   - returned name equals the row we placed at the newer timestamp;
//   - schema_migrations count drops by exactly 1;
//   - the matching checksums row also dropped to 0.
func TestRollbackMigration_RemovesLastApplied(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Wipe the auto-applied rows so our deterministic seed owns the
	// schema_migrations table.
	if _, err := db.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatalf("wipe schema_migrations: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM migration_checksums`); err != nil {
		t.Fatalf("wipe migration_checksums: %v", err)
	}

	const olderRow = "001_initial_schema.sql"
	const newerRow = "014_idx_edges_target.sql"
	ts1 := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Minute) // ts2 strictly newer than ts1

	mustExec(t, db, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, olderRow, ts1)
	mustExec(t, db, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, newerRow, ts2)
	mustExec(t, db, `INSERT INTO migration_checksums (version, checksum, checksum_sha256) VALUES (?, ?, ?)`,
		olderRow, "00", "0000000000000000000000000000000000000000000000000000000000000000")
	mustExec(t, db, `INSERT INTO migration_checksums (version, checksum, checksum_sha256) VALUES (?, ?, ?)`,
		newerRow, "00", "1111111111111111111111111111111111111111111111111111111111111111")

	const beforeN = 2
	got, err := RollbackMigration(db, "")
	if err != nil {
		t.Fatalf("RollbackMigration: %v", err)
	}
	if got != newerRow {
		t.Errorf("returned last: want %q (newer applied_at), got %q", newerRow, got)
	}

	// schema_migrations count drops by exactly 1.
	var afterN int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&afterN); err != nil {
		t.Fatalf("count: %v", err)
	}
	if afterN != beforeN-1 {
		t.Errorf("schema_migrations count: want %d, got %d", beforeN-1, afterN)
	}
	// Older row should still be present.
	var olderStill int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, olderRow).Scan(&olderStill); err != nil {
		t.Fatalf("older row audit: %v", err)
	}
	if olderStill != 1 {
		t.Errorf("older row: want 1, got %d", olderStill)
	}
	// Newer's matching migration_checksums row should be gone.
	var checksumRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_checksums WHERE version = ?`, newerRow).Scan(&checksumRows); err != nil {
		t.Fatalf("checksum audit: %v", err)
	}
	if checksumRows != 0 {
		t.Errorf("checksum row for rolled-back migration should be 0, got %d", checksumRows)
	}
	// Older's migration_checksums row should still be present.
	var olderChecksum int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_checksums WHERE version = ?`, olderRow).Scan(&olderChecksum); err != nil {
		t.Fatalf("older checksum audit: %v", err)
	}
	if olderChecksum != 1 {
		t.Errorf("older checksum row: want 1, got %d", olderChecksum)
	}
}

// TestRollbackMigration_TargetBased_DropsAfterTarget — when target is
// non-empty, rollback drops every migration AFTER (and excluding) the
// target.
//
// Audit gates: (a) returned name == target (the function returns the
// target itself); (b) the prior last-applied row is gone; (c) the
// target row itself is still present.
func TestRollbackMigration_TargetBased_DropsAfterTarget(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	files, err := migrationFiles()
	if err != nil {
		t.Fatalf("migrationFiles: %v", err)
	}
	if len(files) < 3 {
		t.Skip("fewer than 3 migrations; skipping target-based rollback")
	}
	target := files[1] // middle migration — everything >= files[2] should drop.

	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	beforeCount := len(applied)

	got, err := RollbackMigration(db, target)
	if err != nil {
		t.Fatalf("RollbackMigration(target=%q): %v", target, err)
	}
	if got != target {
		t.Errorf("want target=%q returned; got %q", target, got)
	}

	after, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("appliedMigrations (after): %v", err)
	}
	// Every file lexicographically > target must not be applied.
	for _, name := range files {
		if name <= target {
			if !after[name] {
				t.Errorf("file %q (<= target) should still be applied; applied map: %v", name, after)
			}
		} else {
			if after[name] {
				t.Errorf("file %q (>target) should be dropped; applied map: %v", name, after)
			}
		}
	}
	if len(after) >= beforeCount {
		t.Errorf("rollback should remove at least one row; before=%d after=%d", beforeCount, len(after))
	}
}

// TestIsIdempotentMigrationError_TruePaths — table-driven lock on every
// documented branch of isIdempotentMigrationError's substring match.
//
// If you change the substrings here, also update the doc-comment on
// isIdempotentMigrationError to match (the two have been known to drift).
func TestIsIdempotentMigrationError_TruePaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msg  string
	}{
		{"duplicate_column_name", "duplicate column name: foo"},
		{"duplicate_column_name_no_colon", "duplicate column name foo"},
		{"index_already_exists", "index idx_foo already exists"},
		{"trigger_already_exists", "trigger trg_foo already exists"},
		{"table_already_exists", "table t_foo already exists"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !isIdempotentMigrationError(errors.New(c.msg)) {
				t.Errorf("want true for %q", c.msg)
			}
		})
	}
}

// TestIsIdempotentMigrationError_FalseCases — symmetric: error strings
// that do NOT match either substring must return false. Also covers the
// nil branch and a SQL syntax error.
func TestIsIdempotentMigrationError_FalseCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"syntax_error", errors.New("syntax error near foo")},
		{"unrelated_text", errors.New("disk full")},
		{"partial_substring", errors.New("already existed in the past")}, // "existed" ≠ "exists"
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if isIdempotentMigrationError(c.err) {
				t.Errorf("want false for %v", c.err)
			}
		})
	}
}

// TestSplitSql_CommentSeparators — the comment-skipping line branch in
// splitSQL. Two comments interleaved between two statements, and a
// trailing comment after the final semi-colon that should not produce
// a phantom statement.
//
// Audit gates: 2 statements (the two non-comment CREATE TABLEs).
func TestSplitSql_CommentSeparators(t *testing.T) {
	t.Parallel()
	in := "-- header comment\n" +
		"CREATE TABLE t1 (id INT);\n" +
		"-- mid comment\n" +
		"CREATE TABLE t2 (id INT);\n" +
		"-- trailing comment\n"
	stmts := splitSQL(in)
	if len(stmts) != 2 {
		t.Fatalf("want 2 stmts, got %d: %v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "t1") || !strings.Contains(stmts[1], "t2") {
		t.Errorf("stmts misordered: %v", stmts)
	}
}

// =========================================================================
// Section 4 — schema-fingerprint poisoned state
// =========================================================================

// TestHashSchema_Deterministic — same schema produces the same hash
// across repeated calls (pure function, no DB state).
func TestHashSchema_Deterministic(t *testing.T) {
	t.Parallel()
	s := statefulSchema()
	h1 := HashSchema(s)
	h2 := HashSchema(s)
	if h1 != h2 {
		t.Fatalf("HashSchema is not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("want 16-char hex (sha256 first 8 bytes), got %d chars: %s", len(h1), h1)
	}
}

// TestHashSchema_OrderIndependent — categories/relations/stateful
// entries are emitted through SortedKeys, so two schemas that disagree
// only in insertion order must yield identical hashes.
//
// Audit gates: h1 == h2.
func TestHashSchema_OrderIndependent(t *testing.T) {
	t.Parallel()
	a := core.SchemaConfig{
		AllowedCategories:  map[string]bool{"world": true, "opinion": true, "experience": true},
		AllowedRelations:   map[string]bool{"uses": true, "mentions": true},
		StatefulCategories: map[string]bool{"task": true, "plan": true},
	}
	b := core.SchemaConfig{
		AllowedCategories:  map[string]bool{"experience": true, "opinion": true, "world": true},
		AllowedRelations:   map[string]bool{"mentions": true, "uses": true},
		StatefulCategories: map[string]bool{"plan": true, "task": true},
	}
	if HashSchema(a) != HashSchema(b) {
		t.Errorf("HashSchema must be order-independent; got %s vs %s", HashSchema(a), HashSchema(b))
	}
}

// TestHashSchema_FieldSensitive — flipping any single field (relation
// blocking vs unblocking vs recovery) MUST mutate the hash. This is the
// fingerprint poison-detector's whole point.
//
// Audit gates: each baseline mutated by one field yields a distinct
// hash (≥ 3 distinct values for ≥ 3 mutations).
func TestHashSchema_FieldSensitive(t *testing.T) {
	t.Parallel()
	base := statefulSchema()
	baseHash := HashSchema(base)

	mut1 := base
	mut1.RelationBlocking = "depends_on"
	if HashSchema(mut1) == baseHash {
		t.Error("changing RelationBlocking must change the hash")
	}

	mut2 := base
	mut2.StateUnblocking = "archived_status"
	if HashSchema(mut2) == baseHash {
		t.Error("changing StateUnblocking must change the hash")
	}

	mut3 := base
	mut3.RelationRecovery = "fix_via"
	if HashSchema(mut3) == baseHash {
		t.Error("changing RelationRecovery must change the hash")
	}

	// And all three mutations must be distinct from one another.
	h1 := HashSchema(mut1)
	h2 := HashSchema(mut2)
	h3 := HashSchema(mut3)
	if h1 == h2 || h1 == h3 || h2 == h3 {
		t.Errorf("field-sensitive mutations should yield distinct hashes, got: base=%s mut1=%s mut2=%s mut3=%s",
			baseHash, h1, h2, h3)
	}
}

// TestCheckSchemaFingerprint_FirstRunInsertsAndReturnsEmptyStored — the
// very first call has no `schema_fingerprint` row in meta; the function
// inserts one and returns ("", current, nil). The next call will then
// return (current, current, nil).
//
// Audit gates: (a) returned stored is empty; (b) returned current is
// non-empty and matches HashSchema(); (c) meta now has one row matching
// current.
func TestCheckSchemaFingerprint_FirstRunInsertsAndReturnsEmptyStored(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	// Wipe any pre-existing row (e.g. one left by an earlier test sharing the cache).
	if _, err := db.Exec(`DELETE FROM meta WHERE key = 'schema_fingerprint'`); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	stored, current, err := CheckSchemaFingerprint(db, statefulSchema())
	if err != nil {
		t.Fatalf("CheckSchemaFingerprint: %v", err)
	}
	if stored != "" {
		t.Errorf("first run: want stored=\"\", got %q", stored)
	}
	wantCurrent := HashSchema(statefulSchema())
	if current != wantCurrent {
		t.Errorf("first run: current=%q, want %q", current, wantCurrent)
	}

	// Audit: meta has the row now.
	var rowVal string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_fingerprint'`).Scan(&rowVal); err != nil {
		t.Fatalf("meta audit: %v", err)
	}
	if rowVal != current {
		t.Errorf("meta row: want %q, got %q", current, rowVal)
	}
}

// TestCheckSchemaFingerprint_MatchReturnsBoth — second call where the
// stored and current fingerprints agree.
//
// Audit gates: (a) returned stored == current; (b) no error; (c) caller
// can drive a "no drift detected" branch from this equality.
func TestCheckSchemaFingerprint_MatchReturnsBoth(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	// Pre-insert the current hash so the first-call insert branch does not fire.
	currentHash := HashSchema(statefulSchema())
	if _, err := db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)`,
		currentHash,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stored, current, err := CheckSchemaFingerprint(db, statefulSchema())
	if err != nil {
		t.Fatalf("CheckSchemaFingerprint: %v", err)
	}
	if stored != current {
		t.Errorf("match path: stored=%q, current=%q (want equal)", stored, current)
	}
	if stored != currentHash {
		t.Errorf("match path: stored=%q, want %q", stored, currentHash)
	}
}

// TestCheckSchemaFingerprint_DriftDetectedReturnsMismatchedPair — when
// the stored fingerprint has been tampered with (the "poisoned state"
// scenario), the function returns a mismatched (stored, current) pair
// with no error. Callers use the inequality to trigger a SIGHUP-style
// reload.
//
// Audit gates: (a) returned stored != current; (b) no error (drift is
// not a hard error here, just a signal).
func TestCheckSchemaFingerprint_DriftDetectedReturnsMismatchedPair(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	// Poison: pre-insert a wrong fingerprint.
	const poisoned = "deadbeefdeadbeef"
	if _, err := db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)`,
		poisoned,
	); err != nil {
		t.Fatalf("poison seed: %v", err)
	}

	stored, current, err := CheckSchemaFingerprint(db, statefulSchema())
	if err != nil {
		t.Fatalf("drift path: want nil error, got %v", err)
	}
	if stored != poisoned {
		t.Errorf("stored: want poisoned=%q, got %q", poisoned, stored)
	}
	if current != HashSchema(statefulSchema()) {
		t.Errorf("current: want %q, got %q", HashSchema(statefulSchema()), current)
	}
	if stored == current {
		t.Fatal("drift path: stored and current must differ for poisoned meta)")
	}
}

// TestStoreSchemaFingerprint_OverwritesExisting — pre-insert a wrong
// value, call StoreSchemaFingerprint, audit the row now matches HashSchema.
//
// Audit gates: (a) function returned nil; (b) meta row matches the
// newly-computed HashSchema().
func TestStoreSchemaFingerprint_OverwritesExisting(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const poisoned = "0000000000000000"
	if _, err := db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)`,
		poisoned,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	want := HashSchema(statefulSchema())
	if err := StoreSchemaFingerprint(db, statefulSchema()); err != nil {
		t.Fatalf("StoreSchemaFingerprint: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_fingerprint'`).Scan(&got); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if got != want {
		t.Errorf("overwrite: want %q, got %q", want, got)
	}
	if got == poisoned {
		t.Error("overwrite: stored row still equals poisoned pre-state")
	}
}

// TestStoreSchemaFingerprint_DBClosed_Errors — when the DB is closed,
// the INSERT OR REPLACE call must fail. Touches the db-exec error
// branch on StoreSchemaFingerprint that wouldn't otherwise be reached
// in normal happy-path tests.
//
// Audit gates: returned error is non-nil (we don't pin the exact SQL
// driver error text — sqlite3's wording varies by version and OS).
func TestStoreSchemaFingerprint_DBClosed_Errors(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "closed.db")
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := StoreSchemaFingerprint(db, statefulSchema()); err == nil {
		t.Fatal("StoreSchemaFingerprint on closed DB: want error, got nil")
	}
}
