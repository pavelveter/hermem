package evaluation

import "time"

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
