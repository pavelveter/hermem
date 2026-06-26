package checks

import (
	"database/sql"
	"fmt"
)

// VectorReport captures vector index statistics.
type VectorReport struct {
	TotalRows    int
	ConfigDim    int
	StoredDim    int
	DimMismatch  bool
	CategoryDims map[string]int
}

// CheckVector inspects the vector index metadata and id_map/entity embedding dimensions.
func CheckVector(db *sql.DB, configuredDim int) (VectorReport, error) {
	var r VectorReport
	r.ConfigDim = configuredDim
	r.CategoryDims = make(map[string]int)

	// Total rows in id_map (proxy for indexed vectors).
	err := db.QueryRow("SELECT COUNT(*) FROM id_map").Scan(&r.TotalRows)
	if err != nil {
		return r, fmt.Errorf("id_map count: %w", err)
	}

	// Stored embedding dim from meta table.
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&r.StoredDim)
	if err != nil {
		return r, fmt.Errorf("meta embedding_dim: %w", err)
	}
	if r.StoredDim != r.ConfigDim {
		r.DimMismatch = true
	}

	return r, nil
}
