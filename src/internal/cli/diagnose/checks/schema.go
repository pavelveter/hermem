package checks

import (
	"database/sql"
	"fmt"
)

// SchemaReport captures SQLite integrity and foreign-key state.
type SchemaReport struct {
	ForeignKeysOK bool
	OrphanEdges   int
	IntegrityOK   bool
	IntegrityLog  []string
}

// CheckSchema runs integrity_check, foreign_key_check, and orphan edge count.
func CheckSchema(db *sql.DB) (SchemaReport, error) {
	var r SchemaReport
	r.ForeignKeysOK = true
	r.IntegrityOK = true

	// PRAGMA foreign_key_check — returns row triples on violation.
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return r, fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	var violations []string
	for rows.Next() {
		var table, rowid, parent, fkey string
		if err := rows.Scan(&table, &rowid, &parent, &fkey); err != nil {
			return r, fmt.Errorf("foreign_key_check scan: %w", err)
		}
		violations = append(violations, fmt.Sprintf("%s rowid=%s → %s (%s)", table, rowid, parent, fkey))
	}
	if len(violations) > 0 {
		r.ForeignKeysOK = false
		r.IntegrityLog = append(r.IntegrityLog, "foreign_key violations: "+fmt.Sprint(violations))
	}

	// Orphan edges: edges whose source_id or target_id no longer exists in entities.
	var orphanCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM edges e WHERE NOT EXISTS (SELECT 1 FROM entities WHERE id = e.source_id) OR NOT EXISTS (SELECT 1 FROM entities WHERE id = e.target_id)`).Scan(&orphanCount)
	if err != nil {
		return r, fmt.Errorf("orphan edges: %w", err)
	}
	r.OrphanEdges = orphanCount
	if orphanCount > 0 {
		r.IntegrityLog = append(r.IntegrityLog, fmt.Sprintf("orphan edges: %d", orphanCount))
	}

	// PRAGMA integrity_check — returns "ok" on success, or error rows.
	ir, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return r, fmt.Errorf("integrity_check: %w", err)
	}
	defer ir.Close()
	for ir.Next() {
		var msg string
		if err := ir.Scan(&msg); err != nil {
			return r, fmt.Errorf("integrity_check scan: %w", err)
		}
		if msg != "ok" {
			r.IntegrityOK = false
			r.IntegrityLog = append(r.IntegrityLog, "integrity: "+msg)
		}
	}

	return r, nil
}
