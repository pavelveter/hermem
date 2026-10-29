package retrieval

import (
	"context"
	"database/sql"
	"sync"
)

// Prepared-statement cache for the graph-walk query.
//
// Compiling the recursive CTE costs ~60µs — more than ten times the cost of
// executing it on a typical graph — and database/sql re-compiles the SQL on
// every db.Query call. Memoizing the compiled statement per database handle
// keeps that cost off the read path.
//
// Statements are keyed by (handle, query text): several databases can be open
// in one process (tests, multiple stores) and a statement is only valid for the
// handle it was prepared on.

type stmtKey struct {
	db  *sql.DB
	sql string
}

var stmtCache = struct {
	mu sync.Mutex
	m  map[stmtKey]*sql.Stmt
}{m: make(map[stmtKey]*sql.Stmt)}

// maxCachedStmts bounds the cache. Production opens one handle and uses a
// handful of query shapes; the bound only matters for processes that open many
// handles, where dropping everything is cheaper than tracking recency.
const maxCachedStmts = 32

// queryCached runs query with a memoized prepared statement. The statement is
// compiled outside the cache lock: compiling needs a pooled connection, and
// holding the lock while waiting for one can deadlock against a caller that
// already owns the only connection (MaxOpenConns(1)).
func queryCached(ctx context.Context, db *sql.DB, query string, args ...interface{}) (*sql.Rows, error) {
	key := stmtKey{db: db, sql: query}

	stmtCache.mu.Lock()
	stmt := stmtCache.m[key]
	stmtCache.mu.Unlock()

	if stmt == nil {
		prepared, err := db.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		stmtCache.mu.Lock()
		if existing := stmtCache.m[key]; existing != nil {
			// Lost a race with another goroutine: keep the winner.
			stmtCache.mu.Unlock()
			_ = prepared.Close()
			stmt = existing
		} else {
			if len(stmtCache.m) >= maxCachedStmts {
				for k, s := range stmtCache.m {
					_ = s.Close()
					delete(stmtCache.m, k)
				}
			}
			stmtCache.m[key] = prepared
			stmtCache.mu.Unlock()
			stmt = prepared
		}
	}

	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		// The handle may have been closed (tests, shutdown) or the schema may
		// have changed underneath the statement. Drop the entry so the next
		// call compiles a fresh statement instead of failing forever.
		stmtCache.mu.Lock()
		if stmtCache.m[key] == stmt {
			_ = stmt.Close()
			delete(stmtCache.m, key)
		}
		stmtCache.mu.Unlock()
	}
	return rows, err
}
