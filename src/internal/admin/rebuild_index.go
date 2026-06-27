package admin

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

type RebuildIndex struct {
	db       *sql.DB
	vi       VectorIndex
	embedder core.Embedder
	onLog    func(msg string)
}

func NewRebuildIndex(db *sql.DB, vi VectorIndex, em core.Embedder) *RebuildIndex {
	return &RebuildIndex{db: db, vi: vi, embedder: em}
}

func (r *RebuildIndex) Run(ctx context.Context, opts RebuildOpts) (*RebuildReport, error) {
	report := &RebuildReport{}

	query := "SELECT id, content FROM entities WHERE 1=1"
	args := []interface{}{}

	if opts.Category != "" {
		query += " AND category = ?"
		args = append(args, opts.Category)
	}
	if !opts.Since.IsZero() {
		query += " AND updated_at >= ?"
		args = append(args, opts.Since.Format(time.RFC3339))
	}
	if opts.OnlyArchived {
		query += " AND archived = 1"
	} else {
		query += " AND archived = 0"
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("rebuild: query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Sprintf("scan: %v", err))
			continue
		}
		report.Processed++

		if opts.DryRun {
			r.logf("would re-embed %s", id)
			continue
		}

		r.logf("re-embedding %s", id)
		vec, err := r.embedder.Embed(ctx, content)
		if err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Sprintf("embed %s: %v", id, err))
			continue
		}

		if err := r.vi.Remove(ctx, []string{id}); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Sprintf("remove %s: %v", id, err))
			continue
		}
		if err := r.vi.Store(ctx, id, vec); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Sprintf("store %s: %v", id, err))
			continue
		}
		report.Reembedded++
	}

	return report, rows.Err()
}

// OnLog sets a progress-log callback.
func (r *RebuildIndex) OnLog(fn func(msg string)) {
	r.onLog = fn
}

func (r *RebuildIndex) logf(format string, args ...interface{}) {
	if r.onLog != nil {
		r.onLog(fmt.Sprintf(format, args...))
	}
}
