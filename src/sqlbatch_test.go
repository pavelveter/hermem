package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// openChunkTestDB opens a fresh on-disk SQLite database with a single
// integer-keyed table. We use on-disk rather than :memory: so the
// prepared-statement path that enforces SQLITE_MAX_VARIABLE_NUMBER is
// identical to the production code path.
func openChunkTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chunk-test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE ids_only (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	return db
}

// seedChunkIDs inserts ids as rows in ids_only so subsequent DELETE
// statements have something to remove.
func seedChunkIDs(t *testing.T, db *sql.DB, ids []int) {
	t.Helper()
	for _, id := range ids {
		if _, err := db.Exec(`INSERT INTO ids_only (id) VALUES (?)`, id); err != nil {
			t.Fatalf("INSERT %d: %v", id, err)
		}
	}
}

func countChunkRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ids_only`).Scan(&n); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	return n
}

func intsToStrings(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.Itoa(id)
	}
	return out
}

// TestExecInChunksEmptyIsNoop checks the boundary case: nil/empty
// ids should return nil without issuing any SQL. The seed assertion
// confirms we did not accidentally issue a malformed statement.
func TestExecInChunksEmptyIsNoop(t *testing.T) {
	db := openChunkTestDB(t)
	seedChunkIDs(t, db, []int{1, 2, 3})

	if err := execInChunks(context.Background(), db,
		"DELETE FROM ids_only WHERE id IN (%s)", nil, 0); err != nil {
		t.Fatalf("execInChunks(nil): %v", err)
	}
	if err := execInChunks(context.Background(), db,
		"DELETE FROM ids_only WHERE id IN (%s)", []string{}, DefaultSQLBatchSize); err != nil {
		t.Fatalf("execInChunks([]): %v", err)
	}
	if got := countChunkRows(t, db); got != 3 {
		t.Errorf("rows after empty exec = %d, want 3 (no SQL should have run)", got)
	}
}

// TestExecInChunksSingleChunk covers the trivial case where the ID
// count fits in one chunk; the helper should still produce a working
// DELETE.
func TestExecInChunksSingleChunk(t *testing.T) {
	db := openChunkTestDB(t)
	seedChunkIDs(t, db, []int{1, 2, 3})

	if err := execInChunks(context.Background(), db,
		"DELETE FROM ids_only WHERE id IN (%s)",
		intsToStrings([]int{1, 2, 3}), 500); err != nil {
		t.Fatalf("execInChunks: %v", err)
	}
	if got := countChunkRows(t, db); got != 0 {
		t.Errorf("rows after delete = %d, want 0", got)
	}
}

// TestExecInChunksMultipleChunksPartition exercises the partitioning
// logic: 5 ids with chunk size 2 must produce 3 sub-DELETE statements
// (sizes 2, 2, 1). If the chunking math were wrong, either the tail
// would be dropped (residual rows > 0) or the helper would panic on
// an out-of-range slice.
func TestExecInChunksMultipleChunksPartition(t *testing.T) {
	db := openChunkTestDB(t)
	seedChunkIDs(t, db, []int{1, 2, 3, 4, 5})

	if err := execInChunks(context.Background(), db,
		"DELETE FROM ids_only WHERE id IN (%s)",
		intsToStrings([]int{1, 2, 3, 4, 5}), 2); err != nil {
		t.Fatalf("execInChunks: %v", err)
	}
	if got := countChunkRows(t, db); got != 0 {
		t.Errorf("rows after chunked delete = %d, want 0", got)
	}
}

// TestExecInChunksBelowSQLiteVariableLimit is the regression test for
// the original bug: a raw IN-clause with >999 placeholders errors out
// from mattn/go-sqlite3 with "too many SQL variables". With chunking,
// 1000 ids must succeed (2 chunks of 500 each, under the limit).
func TestExecInChunksBelowSQLiteVariableLimit(t *testing.T) {
	db := openChunkTestDB(t)
	const n = 1000
	ids := make([]int, n)
	for i := 0; i < n; i++ {
		ids[i] = i + 1
	}
	seedChunkIDs(t, db, ids)

	if err := execInChunks(context.Background(), db,
		"DELETE FROM ids_only WHERE id IN (%s)",
		intsToStrings(ids), DefaultSQLBatchSize); err != nil {
		t.Fatalf("execInChunks 1000 ids failed: %v", err)
	}
	if got := countChunkRows(t, db); got != 0 {
		t.Errorf("rows after 1000-id delete = %d, want 0", got)
	}
}

// TestExecInChunksBulkUpdate is the production-shape of the metrics
// worker: an UPDATE with an IN-clause on a string-keyed table. Without
// chunking, this would still OOM/parse-error on very large flush sets.
func TestExecInChunksBulkUpdate(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE kv (id TEXT PRIMARY KEY, n INTEGER)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	const n = 750
	for i := 0; i < n; i++ {
		if _, err := db.Exec(`INSERT INTO kv (id, n) VALUES (?, 0)`,
			"key-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("INSERT %d: %v", i, err)
		}
	}
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = "key-" + strconv.Itoa(i)
	}
	if err := execInChunks(context.Background(), db,
		"UPDATE kv SET n = 1 WHERE id IN (%s)",
		ids, DefaultSQLBatchSize); err != nil {
		t.Fatalf("execInChunks: %v", err)
	}
	var updated int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kv WHERE n = 1`).Scan(&updated); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if updated != n {
		t.Errorf("updated rows = %d, want %d", updated, n)
	}
}

// TestExecInChunksPropagatesError verifies that a syntax-failing query
// template surfaces an error to the caller (rather than silently
// completing). Without this guarantee, chunked failures could be lost
// in the batch processing log spam.
func TestExecInChunksPropagatesError(t *testing.T) {
	db := openChunkTestDB(t)
	seedChunkIDs(t, db, []int{1, 2, 3})
	if err := execInChunks(context.Background(), db,
		"THIS IS NOT VALID SQL WITH %s PLACEHOLDER",
		intsToStrings([]int{1, 2, 3}), 500); err == nil {
		t.Fatal("expected error from malformed SQL, got nil")
	}
}
