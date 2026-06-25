package admin

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type IntegrityChecker struct {
	db *sql.DB
}

func NewIntegrityChecker(db *sql.DB) *IntegrityChecker {
	return &IntegrityChecker{db: db}
}

func (c *IntegrityChecker) Check(ctx context.Context) (*IntegrityReport, error) {
	report := &IntegrityReport{CheckedAt: time.Now()}
	var issues []Issue

	issues = append(issues, c.checkMissingEmbeddings(ctx)...)
	issues = append(issues, c.checkDanglingEdges(ctx)...)
	issues = append(issues, c.checkArchiveConsistency(ctx)...)

	report.Issues = issues
	report.OK = !report.CriticalExist()
	return report, nil
}

func (c *IntegrityChecker) checkMissingEmbeddings(ctx context.Context) []Issue {
	rows, err := c.db.QueryContext(ctx,
		`SELECT id, category FROM entities WHERE embedding IS NULL OR length(embedding) = 0 LIMIT 100`)
	if err != nil {
		return []Issue{{
			Code:    "MISSING_EMBEDDING_QUERY_ERR",
			Level:   IssueWarning,
			Message: fmt.Sprintf("query failed: %v", err),
		}}
	}
	defer rows.Close()

	var issues []Issue
	var count int
	for rows.Next() {
		var id, cat string
		if err := rows.Scan(&id, &cat); err != nil {
			continue
		}
		count++
		level := IssueWarning
		if count > 5 {
			level = IssueInfo
		}
		issues = append(issues, Issue{
			Code:    "MISSING_EMBEDDING",
			Level:   level,
			Subject: id,
			Message: fmt.Sprintf("entity %q (%s) has no embedding", id, cat),
		})
	}
	if len(issues) == 0 {
		return nil
	}
	// Escalate to critical if too many
	if count >= 10 {
		issues = []Issue{{
			Code:    "MISSING_EMBEDDING",
			Level:   IssueCritical,
			Subject: fmt.Sprintf("%d entities", count),
			Message: fmt.Sprintf("%d entities are missing embeddings (coverage issue)", count),
		}}
	}
	return issues
}

func (c *IntegrityChecker) checkDanglingEdges(ctx context.Context) []Issue {
	rows, err := c.db.QueryContext(ctx,
		`SELECT e.source_id, e.target_id, e.relation_type
		 FROM edges e
		 WHERE NOT EXISTS (SELECT 1 FROM entities WHERE id = e.source_id)
		 OR NOT EXISTS (SELECT 1 FROM entities WHERE id = e.target_id)
		 LIMIT 100`)
	if err != nil {
		return []Issue{{
			Code:    "DANGLING_EDGE_QUERY_ERR",
			Level:   IssueWarning,
			Message: fmt.Sprintf("query failed: %v", err),
		}}
	}
	defer rows.Close()

	var issues []Issue
	for rows.Next() {
		var src, tgt, rt string
		if err := rows.Scan(&src, &tgt, &rt); err != nil {
			continue
		}
		issues = append(issues, Issue{
			Code:    "DANGLING_EDGE",
			Level:   IssueCritical,
			Subject: fmt.Sprintf("%s -> %s", src, tgt),
			Message: fmt.Sprintf("edge (%s) references non-existent entity", rt),
		})
	}
	return issues
}

func (c *IntegrityChecker) checkArchiveConsistency(ctx context.Context) []Issue {
	rows, err := c.db.QueryContext(ctx,
		`SELECT id, content FROM entities WHERE archived = 1 AND embedding IS NOT NULL AND length(embedding) > 0 LIMIT 100`)
	if err != nil {
		return []Issue{{
			Code:    "ARCHIVE_CONSISTENCY_QUERY_ERR",
			Level:   IssueWarning,
			Message: fmt.Sprintf("query failed: %v", err),
		}}
	}
	defer rows.Close()

	var issues []Issue
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			continue
		}
		short := id
		if len(short) > 80 {
			short = short[:80]
		}
		issues = append(issues, Issue{
			Code:    "ARCHIVE_CONSISTENCY",
			Level:   IssueWarning,
			Subject: id,
			Message: fmt.Sprintf("archived entity %q still has embedding (stale vector index entry)", short),
		})
	}
	return issues
}
