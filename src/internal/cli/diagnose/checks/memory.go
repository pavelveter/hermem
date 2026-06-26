package checks

import (
	"database/sql"
	"fmt"
)

// MemoryReport captures embedding density and belief subsystem stats.
type MemoryReport struct {
	TotalEntities         int
	EntitiesWithEmbedding int
	EmbeddingDensity      float64
	DensityByCategory     map[string]float64
	BeliefCounts          map[string]int
}

// CheckMemory runs embedding density and beliefs table diagnostics.
func CheckMemory(db *sql.DB) (MemoryReport, error) {
	var r MemoryReport
	r.DensityByCategory = make(map[string]float64)
	r.BeliefCounts = make(map[string]int)

	// Total entities.
	err := db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&r.TotalEntities)
	if err != nil {
		return r, fmt.Errorf("total entities: %w", err)
	}

	// Entities with non-nil embedding.
	err = db.QueryRow("SELECT COUNT(*) FROM entities WHERE embedding IS NOT NULL").Scan(&r.EntitiesWithEmbedding)
	if err != nil {
		return r, fmt.Errorf("entities with embedding: %w", err)
	}
	if r.TotalEntities > 0 {
		r.EmbeddingDensity = float64(r.EntitiesWithEmbedding) / float64(r.TotalEntities) * 100
	}

	// Density by category.
	rows, err := db.Query("SELECT category, COUNT(*) as total, SUM(CASE WHEN embedding IS NOT NULL THEN 1 ELSE 0 END) as with_emb FROM entities GROUP BY category")
	if err != nil {
		return r, fmt.Errorf("density by category: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var total, withEmb int
		if err := rows.Scan(&cat, &total, &withEmb); err != nil {
			return r, fmt.Errorf("density scan: %w", err)
		}
		pct := 0.0
		if total > 0 {
			pct = float64(withEmb) / float64(total) * 100
		}
		r.DensityByCategory[cat] = pct
	}

	// Belief counts by status (beliefs table from migration 008).
	bRows, err := db.Query("SELECT status, COUNT(*) FROM beliefs GROUP BY status")
	if err != nil {
		r.BeliefCounts["error"] = 1
		r.BeliefCounts["note"] = 0
		return r, nil // beliefs table may not exist; not fatal
	}
	defer bRows.Close()
	for bRows.Next() {
		var status string
		var count int
		if err := bRows.Scan(&status, &count); err != nil {
			return r, fmt.Errorf("belief scan: %w", err)
		}
		r.BeliefCounts[status] = count
	}

	return r, nil
}
