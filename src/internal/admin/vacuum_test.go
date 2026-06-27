package admin

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestVacuumRunner_Run(t *testing.T) {
	f, err := os.CreateTemp("", "hermem-test-vac-*.db")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	_ = f.Close()
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	defer os.Remove(f.Name()) //nolint:errcheck // t.Cleanup best-effort: missing file isn't a test failure indicator

	db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, data TEXT)`)
	for i := 0; i < 1000; i++ {
		db.Exec("INSERT INTO t (data) VALUES (?)", randomString(200))
	}
	// Delete half to create free pages
	db.Exec("DELETE FROM t WHERE id % 2 = 0")

	var callCount int
	vr := NewVacuumRunner(db)
	vr.OnProgress(func(pct int, reclaimed int64) {
		callCount++
	})

	reclaimed, err := vr.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reclaimed <= 0 {
		t.Errorf("expected reclaimed > 0, got %d", reclaimed)
	}
	if callCount == 0 {
		t.Error("expected progress callback to be called at least once")
	}
}

func TestVacuumRunner_EmptyDB(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	vr := NewVacuumRunner(db)
	reclaimed, err := vr.Run(t.Context())
	if err != nil {
		t.Fatalf("Run empty: %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("empty DB: expected 0 reclaimed, got %d", reclaimed)
	}
}

func randomString(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[int64(i)%int64(len(chars))]
	}
	return string(b)
}
