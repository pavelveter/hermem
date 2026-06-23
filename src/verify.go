package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// VerifyReport captures the result of a single VerifyGraph run. Every
// field holds a count — for validity checks (CorruptBlobs, OrphanEdges,
// InvalidStatus, InvalidRelType) zero means "clean".
type VerifyReport struct {
	Entities       int
	Edges          int
	Archived       int
	Embeddings     int
	CorruptBlobs   int
	OrphanEdges    int
	InvalidStatus  int
	InvalidRelType int
}

// VerifyGraph runs read-only sanity checks against the SQLite database.
//
// Checks performed:
//  1. Base counts (entities, edges, archived, with-embedding).
//  2. Corrupt embedding blobs — length != dim*4.
//  3. Orphan edges — source_id or target_id not in entities.
//  4. Invalid stateful status — stateful category with NULL / unknown status.
//  5. Invalid relation_type — edges whose type is not in allowed_relations.
//
// All queries are read-only. In production (FK ON from Sprint 1), checks
// 3 and 5 should always return 0 — they exist as a safety net for data
// corruption or manual DB edits.
func VerifyGraph(db *sql.DB, schema SchemaConfig, dim int) (*VerifyReport, error) {
	r := &VerifyReport{}

	if err := countRow(db, "SELECT COUNT(*) FROM entities", &r.Entities); err != nil {
		return nil, fmt.Errorf("count entities: %w", err)
	}
	if err := countRow(db, "SELECT COUNT(*) FROM edges", &r.Edges); err != nil {
		return nil, fmt.Errorf("count edges: %w", err)
	}
	if err := countRow(db, "SELECT COUNT(*) FROM entities WHERE archived = 1", &r.Archived); err != nil {
		return nil, fmt.Errorf("count archived: %w", err)
	}
	if err := countRow(db, "SELECT COUNT(*) FROM entities WHERE embedding IS NOT NULL", &r.Embeddings); err != nil {
		return nil, fmt.Errorf("count embeddings: %w", err)
	}

	if err := countRow(db,
		fmt.Sprintf("SELECT COUNT(*) FROM entities WHERE embedding IS NOT NULL AND length(embedding) != %d", dim*4),
		&r.CorruptBlobs); err != nil {
		return nil, fmt.Errorf("count corrupt blobs: %w", err)
	}

	if err := countRow(db, `
		SELECT COUNT(*) FROM edges e
		WHERE NOT EXISTS (SELECT 1 FROM entities WHERE id = e.source_id)
		   OR NOT EXISTS (SELECT 1 FROM entities WHERE id = e.target_id)
	`, &r.OrphanEdges); err != nil {
		return nil, fmt.Errorf("count orphan edges: %w", err)
	}

	if len(schema.StatefulCategories) > 0 && len(schema.ValidStates) > 0 {
		catPH, catArgs := boolMapInClause(schema.StatefulCategories)
		statePH, stateArgs := boolMapInClause(schema.ValidStates)
		if catPH != "" && statePH != "" {
			args := append(append([]interface{}{}, catArgs...), stateArgs...)
			if err := countRow(db, fmt.Sprintf(`
				SELECT COUNT(*) FROM entities
				WHERE category IN (%s)
				  AND archived = 0
				  AND (status IS NULL OR status NOT IN (%s))
			`, catPH, statePH), &r.InvalidStatus, args...); err != nil {
				return nil, fmt.Errorf("count invalid status: %w", err)
			}
		}
	}

	if len(schema.AllowedRelations) > 0 {
		catPH, catArgs := boolMapInClause(schema.AllowedRelations)
		if catPH != "" {
			if err := countRow(db, fmt.Sprintf(`
				SELECT COUNT(*) FROM edges WHERE relation_type NOT IN (%s)
			`, catPH), &r.InvalidRelType, catArgs...); err != nil {
				return nil, fmt.Errorf("count invalid relation: %w", err)
			}
		}
	}

	return r, nil
}

// Pass reports whether every integrity check passed (all failure counts = 0).
func (r *VerifyReport) Pass() bool {
	return r.CorruptBlobs == 0 &&
		r.OrphanEdges == 0 &&
		r.InvalidStatus == 0 &&
		r.InvalidRelType == 0
}

// String returns a human-readable multi-line report suitable for
// printing to a terminal. Each line is labeled, right-padded so the
// count aligns at column 32. The final line reports Status: OK or
// FAIL with a summary.
func (r *VerifyReport) String() string {
	var sb strings.Builder
	sb.WriteString("Graph integrity report\n")

	writeCount(&sb, "entities", r.Entities)
	writeCount(&sb, "edges", r.Edges)
	writeCount(&sb, "archived entities", r.Archived)
	writeCount(&sb, "embeddings", r.Embeddings)

	writeCheck(&sb, "corrupt embedding blobs", r.CorruptBlobs)
	writeCheck(&sb, "orphan edges", r.OrphanEdges)
	writeCheck(&sb, "invalid status values", r.InvalidStatus)
	writeCheck(&sb, "invalid relation types", r.InvalidRelType)

	if r.Pass() {
		sb.WriteString("Status: OK\n")
	} else {
		sb.WriteString(fmt.Sprintf("Status: FAIL (%d issue(s))\n",
			r.CorruptBlobs+r.OrphanEdges+r.InvalidStatus+r.InvalidRelType))
	}
	return sb.String()
}

func writeCount(sb *strings.Builder, label string, n int) {
	fmt.Fprintf(sb, "  %-27s %d\n", label+":", n)
}

func writeCheck(sb *strings.Builder, label string, n int) {
	if n == 0 {
		fmt.Fprintf(sb, "  %-27s %d  OK\n", label+":", n)
	} else {
		fmt.Fprintf(sb, "  %-27s %d  FAIL\n", label+":", n)
	}
}

func countRow(db *sql.DB, query string, dest *int, args ...interface{}) error {
	return db.QueryRow(query, args...).Scan(dest)
}
