package evaluation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Report holds the results of a benchmark run.
type Report struct {
	Dataset      string    `json:"dataset"`
	Recall       float64   `json:"recall"`
	Precision    float64   `json:"precision"`
	MRR          float64   `json:"mrr"`
	NDCG         float64   `json:"ndcg"`
	TotalQueries int       `json:"total_queries"`
	K            int       `json:"k"`
	RunAt        time.Time `json:"run_at"`
}

// Format returns a human-readable multi-line report.
func (r Report) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== Benchmark Report ===\n")
	fmt.Fprintf(&b, "Dataset:      %s\n", r.Dataset)
	fmt.Fprintf(&b, "Queries:      %d\n", r.TotalQueries)
	fmt.Fprintf(&b, "K:            %d\n", r.K)
	fmt.Fprintf(&b, "Run at:       %s\n", r.RunAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "--- Metrics ---\n")
	fmt.Fprintf(&b, "Recall@%d:    %.4f\n", r.K, r.Recall)
	fmt.Fprintf(&b, "Precision@%d: %.4f\n", r.K, r.Precision)
	fmt.Fprintf(&b, "MRR:          %.4f\n", r.MRR)
	fmt.Fprintf(&b, "NDCG@%d:      %.4f\n", r.K, r.NDCG)
	return b.String()
}

// JSON returns the report as indented JSON bytes.
func (r Report) JSON() []byte {
	out, _ := json.MarshalIndent(r, "", "  ") //nolint:errcheck // JSON() is best-effort formatting; marshal failure strips JSON output but keeps the human-readable Format() path intact
	return out
}
