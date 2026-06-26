// Package evaluation — retrieval benchmark dataset.
package evaluation

import "context"

// DefaultRetrievalDataset returns a curated retrieval dataset covering
// entity lookups, multi-match queries, and empty-result scenarios.
//
// The dataset is self-contained: qrels define expected relevant doc-ids
// per query; a RetrievalFn (the system under test) must return ranked
// doc-ids that the Runner scores against these qrels.
func DefaultRetrievalDataset() Dataset {
	return Dataset{
		Name: "default-retrieval-v1",
		QueryIDs: []string{
			"q-go-preference",
			"q-multi-language",
			"q-os",
			"q-editor",
			"q-empty",
			"q-single-match",
			"q-two-matches",
		},
		Qrels: map[string][]string{
			// Single relevant doc
			"q-go-preference":   {"doc-go"},
			// Multiple relevant docs
			"q-multi-language":  {"doc-go", "doc-python", "doc-rust"},
			// One relevant doc in a list
			"q-os":              {"doc-linux"},
			// One relevant doc
			"q-editor":         {"doc-vim"},
			// No relevant docs — tests zero-recall handling
			"q-empty":          {},
			// Exactly one match
			"q-single-match":   {"doc-go"},
			// Two relevant docs — tests partial recall
			"q-two-matches":    {"doc-go", "doc-python"},
		},
	}
}

// PerfectRetrievalFn returns a RetrievalFn that always returns the
// exact qrels list in order — a perfect retriever for smoke-testing
// the metrics themselves.
func PerfectRetrievalFn(qrels map[string][]string) RetrievalFn {
	return func(_ context.Context, qid string) ([]string, error) {
		return qrels[qid], nil
	}
}
