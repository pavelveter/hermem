package evaluation

import (
	"context"
	"fmt"
	"time"
)

// QueryResult holds the retrieval output for a single query.
type QueryResult struct {
	QueryID string
	DocIDs  []string
}

// RetrievalFn is the function under test: given a query-id, return ranked doc-ids.
type RetrievalFn func(ctx context.Context, queryID string) ([]string, error)

// Dataset bundles qrels and query IDs for a benchmark run.
type Dataset struct {
	Name     string
	Qrels    map[string][]string
	QueryIDs []string
}

// EvalRunner executes a retrieval function against a dataset and computes metrics.
type EvalRunner struct {
	K int // top-K cutoff for all metrics
}

// NewEvalRunner creates an EvalRunner with the given K.
func NewEvalRunner(k int) *EvalRunner {
	return &EvalRunner{K: k}
}

// Run executes fn against dataset, computes all four metrics, and returns a Report.
func (r *EvalRunner) Run(ctx context.Context, dataset Dataset, fn RetrievalFn) (Report, error) {
	if r.K <= 0 {
		r.K = 10
	}

	results := make(map[string][]string, len(dataset.QueryIDs))

	for _, qid := range dataset.QueryIDs {
		select {
		case <-ctx.Done():
			return Report{}, fmt.Errorf("evaluation cancelled: %w", ctx.Err())
		default:
		}

		docs, err := fn(ctx, qid)
		if err != nil {
			return Report{}, fmt.Errorf("query %s: %w", qid, err)
		}
		results[qid] = docs
	}

	return Report{
		Dataset:      dataset.Name,
		Recall:       Recall(dataset.Qrels, results, r.K),
		Precision:    Precision(dataset.Qrels, results, r.K),
		MRR:          MRR(dataset.Qrels, results),
		NDCG:         NDCG(dataset.Qrels, results, r.K),
		TotalQueries: len(dataset.QueryIDs),
		K:            r.K,
		RunAt:        time.Now(),
	}, nil
}
