package consolidation

import "time"

// Report is the envelope returned by RunOnce/Consolidate.
type Report struct {
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	NodesClustered   int       `json:"nodes_clustered"`
	NodesSynthesized int       `json:"nodes_synthesized"`
	NodesArchived    int       `json:"nodes_archived"`
	DurationMs       int64     `json:"duration_ms"`
	Error            string    `json:"error,omitempty"`
}

// Aliases for compatibility with callers/tests that use other names.
type ConsolidationReport = Report
type ConsolidationStats = Report
type Stats = Report
