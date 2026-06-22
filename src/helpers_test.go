package main

import (
	"database/sql"
	"testing"
)

// memDB returns a fresh in-memory SQLite handle and an in-memory
// VectorIndex for unit tests. In-memory keeps tests fast and fully
// isolated; verify_test.go's file-based DBs stay for integration /
// timing tests that need real I/O. The helper is in helpers_test.go
// so every _test.go file in package main can
// `db, vi := memDB(t)` without redeclaration.
func memDB(t testing.TB) (*sql.DB, VectorIndex) {
	t.Helper()
	db, err := InitDB(":memory:", 768)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	vi := newVectorIndex("in-memory", db, 768)
	return db, vi
}
