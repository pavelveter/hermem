package admin

import "time"

type Stats struct {
	NodeCount         int64     `json:"node_count"`
	EdgeCount         int64     `json:"edge_count"`
	ArchivedCount     int64     `json:"archived_count"`
	ContradictionCount int64    `json:"contradiction_count"`
	EmbeddingCoverage float64   `json:"embedding_coverage"`
	LastGCRunAt       time.Time `json:"last_gc_run_at"`
	LastGCArchived    int64     `json:"last_gc_archived"`
	DBSizeBytes       int64     `json:"db_size_bytes"`
	CapturedAt        time.Time `json:"captured_at"`
}

type IssueLevel string

const (
	IssueCritical IssueLevel = "critical"
	IssueWarning  IssueLevel = "warning"
	IssueInfo     IssueLevel = "info"
)

type Issue struct {
	Code    string     `json:"code"`
	Level   IssueLevel `json:"level"`
	Subject string     `json:"subject"`
	Message string     `json:"message"`
}

type IntegrityReport struct {
	OK        bool      `json:"ok"`
	Issues    []Issue   `json:"issues"`
	CheckedAt time.Time `json:"checked_at"`
}

func (r *IntegrityReport) CriticalExist() bool {
	for i := range r.Issues {
		if r.Issues[i].Level == IssueCritical {
			return true
		}
	}
	return false
}
