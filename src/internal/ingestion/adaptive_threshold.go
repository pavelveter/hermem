package ingestion

import (
	"database/sql"
	"math"
)

// AdaptiveLinkThreshold computes a similarity threshold for auto-linking
// entities based on local graph density. Dense neighborhoods require
// stronger similarity before creating new edges to avoid over-linking.
//
// The base threshold is scaled up by a factor derived from the average
// degree of the candidate's neighbours: higher density → higher bar.
//
// This function is intentionally pure (no side effects) and currently
// DISABLED by default. The ingest pipeline passes a fixed threshold
// unless the operator explicitly opts in via config. Enable when
// production metrics demonstrate measurable over-linking in dense
// subgraphs.
func AdaptiveLinkThreshold(db *sql.DB, entityID string, baseThreshold float32) float32 {
	avgDeg := avgNeighborDegree(db, entityID)
	if avgDeg <= 0 {
		return baseThreshold
	}
	// Scale factor: 1.0 at avgDeg=0, approaching 1.2 at avgDeg=10+.
	// Capped so the threshold never exceeds baseThreshold * 1.2.
	scale := 1.0 + 0.2*(1.0-math.Exp(-float64(avgDeg)/10.0))
	t := float32(float64(baseThreshold) * scale)
	if t > 1.0 {
		t = 1.0
	}
	return t
}

// avgNeighborDegree returns the average degree of the neighbours of
// entityID. Returns 0 when the entity has no neighbours or the query
// fails (graceful degradation — callers fall back to the base threshold).
func avgNeighborDegree(db *sql.DB, entityID string) float32 {
	if db == nil || entityID == "" {
		return 0
	}
	var avg float32
	err := db.QueryRow(`
		SELECT COALESCE(AVG(e2.degree), 0)
		FROM edges ed
		JOIN entities e2 ON (
			CASE WHEN ed.source_id = ? THEN ed.target_id = e2.id
			ELSE ed.source_id = e2.id
		END)
		WHERE ed.source_id = ? OR ed.target_id = ?
	`, entityID, entityID, entityID).Scan(&avg)
	if err != nil {
		return 0
	}
	return avg
}
