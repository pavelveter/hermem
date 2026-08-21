package domain

// RankingWeight holds tunable parameters for the composite ranker.
// Zero fields are treated as "unset" — call WithDefaults to resolve a
// zero-means-unset struct into one safe to feed the ranker.
type RankingWeight struct {
	VectorWeight          float32
	RecencyWeight         float32
	DepthPenalty          float32
	RecencyHalfLifeHours  float32
	TemporalWeight        float32
	TemporalHalfLifeHours float32
	CentralityWeight      float32
}

// WithDefaults returns w with zero-valued fields replaced by the canonical
// ranking defaults (see ADR-022). This is the single source of truth for
// the default values; both config/ini.go (after LoadConfigFromBinaryDir)
// and retrieval/walk.go call it to finalize a partially-populated weight.
func (w RankingWeight) WithDefaults() RankingWeight {
	if w.VectorWeight == 0 {
		w.VectorWeight = 0.7
	}
	if w.RecencyWeight == 0 {
		w.RecencyWeight = 0.3
	}
	if w.DepthPenalty == 0 {
		w.DepthPenalty = 0.05
	}
	// CentralityWeight is unconditionally applied (no recency half-life
	// comparison needed): a zero value means "do not consider centrality";
	// the canonical default is 0.05 — small enough to nudge ranking
	// without dominating recency or vector similarity.
	if w.CentralityWeight == 0 {
		w.CentralityWeight = 0.05
	}
	if w.RecencyHalfLifeHours == 0 {
		w.RecencyHalfLifeHours = 720
	}
	if w.TemporalHalfLifeHours == 0 {
		w.TemporalHalfLifeHours = 720
	}
	return w
}
