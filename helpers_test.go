package main

import (
	"database/sql"
	"testing"
)

// memDB returns a fresh in-memory SQLite handle wrapped in *sql.DB.
// In-memory keeps unit tests fast and fully isolated; verify_test.go's
// file-based DBs stay for the integration / timing tests that need
// real I/O. The helper is in helpers_test.go so every _test.go file
// in package main can `db := memDB(t)` without redeclaration.
func memDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	return db
}
