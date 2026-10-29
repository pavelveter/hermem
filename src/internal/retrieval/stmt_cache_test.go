package retrieval

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

// TestQueryCached_ReusesStatementAndHonorsArgs guards the cache's two
// invariants: repeated calls with the same query text reuse one statement, and
// different arguments still bind to that statement correctly.
func TestQueryCached_ReusesStatementAndHonorsArgs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	const q = `SELECT ?`

	if got := scanInt(t, ctx, db, q, 7); got != 7 {
		t.Fatalf("first call: want 7, got %d", got)
	}
	if got := scanInt(t, ctx, db, q, 9); got != 9 {
		t.Fatalf("second call with new args: want 9, got %d", got)
	}

	stmtCache.mu.Lock()
	cached := len(stmtCache.m)
	stmtCache.mu.Unlock()
	if cached == 0 {
		t.Fatal("statement was not cached")
	}
}

// TestQueryCached_EvictsStatementOnClosedHandle — a closed handle must not
// leave a dead statement behind that poisons every later call.
func TestQueryCached_EvictsStatementOnClosedHandle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	const q = `SELECT 1`

	scanInt(t, ctx, db, q, nil)

	key := stmtKey{db: db, sql: q}
	stmtCache.mu.Lock()
	_, cached := stmtCache.m[key]
	stmtCache.mu.Unlock()
	if !cached {
		t.Fatal("statement was not cached")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if _, err := queryCached(ctx, db, q); err == nil {
		t.Fatal("query on closed handle: want error, got nil")
	}

	stmtCache.mu.Lock()
	_, stillCached := stmtCache.m[key]
	stmtCache.mu.Unlock()
	if stillCached {
		t.Fatal("dead statement left in cache after handle close")
	}
}

// TestQueryCached_ConcurrentUse — the cache is shared state; -race must stay
// clean and every call must see its own arguments.
func TestQueryCached_ConcurrentUse(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	const q = `SELECT ?`

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				want := g*100 + i
				if got := scanInt(t, ctx, db, q, want); got != want {
					t.Errorf("want %d, got %d", want, got)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func scanInt(t *testing.T, ctx context.Context, db *sql.DB, q string, arg interface{}) int {
	t.Helper()
	var args []interface{}
	if arg != nil {
		args = append(args, arg)
	}
	rows, err := queryCached(ctx, db, q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got int
	if rows.Next() {
		if err := rows.Scan(&got); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return got
}
