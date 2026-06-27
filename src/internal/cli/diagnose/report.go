package diagnose

import "encoding/json"

// Report holds the full self-diagnosis output.
type Report struct {
	Schema    SchemaReport    `json:"schema"`
	Vector    VectorReport    `json:"vector"`
	Memory    MemoryReport    `json:"memory"`
	Retention RetentionReport `json:"retention"`
	Errors    ErrorsReport    `json:"errors"`
}

// SchemaReport captures SQLite integrity checks.
type SchemaReport struct {
	ForeignKeysOK bool     `json:"foreign_keys_ok"`
	OrphanEdges   int      `json:"orphan_edges"`
	IntegrityOK   bool     `json:"integrity_ok"`
	IntegrityLog  []string `json:"integrity_log,omitempty"`
}

// VectorReport captures vector index statistics.
type VectorReport struct {
	TotalRows    int            `json:"total_rows"`
	ConfigDim    int            `json:"config_dim"`
	StoredDim    int            `json:"stored_dim"`
	DimMismatch  bool           `json:"dim_mismatch"`
	CategoryDims map[string]int `json:"category_dims,omitempty"`
}

// MemoryReport captures embedding density and belief subsystem stats.
type MemoryReport struct {
	TotalEntities         int                `json:"total_entities"`
	EntitiesWithEmbedding int                `json:"entities_with_embedding"`
	EmbeddingDensity      float64            `json:"embedding_density_pct"`
	DensityByCategory     map[string]float64 `json:"density_by_category"`
	BeliefCounts          map[string]int     `json:"belief_counts_by_status"`
}

// RetentionReport captures archive state.
type RetentionReport struct {
	ArchivedEntities int     `json:"archived_entities"`
	TotalEntities    int     `json:"total_entities"`
	ArchivedPct      float64 `json:"archived_pct"`
}

// ErrorsReport captures recent error tail.
type ErrorsReport struct {
	Entries []string `json:"entries,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// JSON returns the report as indented JSON bytes.
func (r *Report) JSON() []byte {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return b
}
