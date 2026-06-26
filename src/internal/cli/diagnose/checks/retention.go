package checks

import (
	"database/sql"
	"fmt"
)

// RetentionReport captures archive state.
type RetentionReport struct {
	ArchivedEntities int
	TotalEntities    int
	ArchivedPct      float64
}

// CheckRetention counts archived vs total entities.
func CheckRetention(db *sql.DB) (RetentionReport, error) {
	var r RetentionReport

	err := db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&r.TotalEntities)
	if err != nil {
		return r, fmt.Errorf("total entities: %w", err)
	}

	err = db.QueryRow("SELECT COUNT(*) FROM entities WHERE archived = 1").Scan(&r.ArchivedEntities)
	if err != nil {
		return r, fmt.Errorf("archived entities: %w", err)
	}

	if r.TotalEntities > 0 {
		r.ArchivedPct = float64(r.ArchivedEntities) / float64(r.TotalEntities) * 100
	}

	return r, nil
}
