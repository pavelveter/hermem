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
	defer os.Remove(f.Name()) //nolint:errcheck // t.Cleanup: missing file isn't a test failure indicator

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

// TestVacuumRunner_Idempotent verifies vacuum is idempotent — running
// it twice does not decrease entity count or error.
func TestVacuumRunner_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, data TEXT)`)
	for i := 0; i < 500; i++ {
		db.Exec("INSERT INTO t (data) VALUES (?)", randomString(100))
	}

	vr := NewVacuumRunner(db)
	if _, err := vr.Run(t.Context()); err != nil {
		t.Fatalf("first vacuum: %v", err)
	}

	var count int
	_ = db.QueryRow("SELECT count(*) FROM t").Scan(&count)
	if count != 500 {
		t.Fatalf("entity count changed after vacuum: got %d, want 500", count)
	}

	if _, err := vr.Run(t.Context()); err != nil {
		t.Fatalf("second vacuum: %v", err)
	}

	_ = db.QueryRow("SELECT count(*) FROM t").Scan(&count)
	if count != 500 {
		t.Fatalf("entity count changed after second vacuum: got %d, want 500", count)
	}
}

// TestVacuumRunner_Property_EntityCountNeverDecreases is a property test:
// for any sequence of inserts + deletes, vacuum never decreases entity count.
func TestVacuumRunner_Property_EntityCountNeverDecreases(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, data TEXT)`)

	scenarios := []struct {
		name   string
		insert int
		delete int
	}{
		{"no_data", 0, 0},
		{"insert_only", 100, 0},
		{"insert_delete_half", 200, 100},
		{"insert_delete_all", 50, 50},
		{"large_batch", 5000, 2500},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for i := 0; i < sc.insert; i++ {
				db.Exec("INSERT INTO t (data) VALUES (?)", randomString(50))
			}
			if sc.delete > 0 {
				db.Exec("DELETE FROM t WHERE id IN (SELECT id FROM t ORDER BY id LIMIT ?)", sc.delete)
			}

			var before int
			_ = db.QueryRow("SELECT count(*) FROM t").Scan(&before)

			vr := NewVacuumRunner(db)
			if _, err := vr.Run(t.Context()); err != nil {
				t.Fatalf("vacuum: %v", err)
			}

			var after int
			_ = db.QueryRow("SELECT count(*) FROM t").Scan(&after)

			if after != before {
				t.Errorf("vacuum changed entity count: before=%d after=%d", before, after)
			}
		})
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
