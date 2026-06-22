package main

import (
	"context"
	"database/sql"
	"fmt"
)

// DefaultSQLBatchSize is the maximum number of host-parameters per
// exec/query statement. SQLite's compile-time default for
// SQLITE_MAX_VARIABLE_NUMBER is 999 (mattn/go-sqlite3 does not raise
// it). Batch Delete/Update operations split their IN-clause into chunks
// of this size, leaving headroom for WHERE clauses that append their
// own "?" parameters and for any future raise of the limit.
const DefaultSQLBatchSize = 500

// execInChunks executes an Exec-style statement for every disjoint
// chunk of ids. The query template `queryFmt` must contain exactly one
// `%s` placeholder, which is replaced with a comma-separated list of
// `?` placeholders sized to the current chunk.
//
// Why chunking is mandatory: SQLite bounds the number of host
// parameters per prepared statement (SQLITE_MAX_VARIABLE_NUMBER).
// When `len(ids)` exceeds that bound, the original single-statement
// IN(...) form fails with "too many SQL variables". Splitting into
// chunks sized to DefaultSQLBatchSize keeps every statement well
// below the limit.
//
// Returns the first error encountered, wrapped with the chunk range
// to aid root-causing. Empty ids returns nil immediately (no-op).
// chunkSize <= 0 falls back to DefaultSQLBatchSize.
//
// This helper is the production-grade replacement for the inline
// `fmt.Sprintf("WHERE id IN (%s)", strings.Join(placeholders, ","))`
// pattern that previously appeared in vector_sqlitevec.go,
// retention.go, and metrics_worker.go — those call sites used the
// pattern blindly and would explode on `batch_size > 999`.
func execInChunks(ctx context.Context, db *sql.DB, queryFmt string, ids []string, chunkSize int) error {
	if len(ids) == 0 {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = DefaultSQLBatchSize
	}
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		phs, args := inClauseArgs(chunk)
		q := fmt.Sprintf(queryFmt, phs)
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("execInChunks chunk [%d-%d] of %d: %w", start, end, len(ids), err)
		}
	}
	return nil
}
