// Package evaluation — reranker benchmark dataset.
package evaluation

// RankedDoc is one candidate document with a human-assigned relevance
// label (0 = irrelevant, 1 = marginally relevant, 2 = relevant).
type RankedDoc struct {
	DocID     string
	Content   string
	Relevance int // 0, 1, or 2
}

// RerankerQuery bundles a query with its candidate documents and
// the ideal (relevance-sorted) document order.
type RerankerQuery struct {
	QueryID    string
	Query      string
	Candidates []RankedDoc
	// IdealOrder is the sorted list of DocIDs ordered by descending
	// Relevance — the ground-truth ranking.
	IdealOrder []string
}

// RerankerDataset bundles a set of reranking queries for evaluation.
type RerankerDataset struct {
	Name    string
	Queries []RerankerQuery
}

// DefaultRerankerDataset returns a curated dataset covering basic
// reranking scenarios: single-best, ties, all-irrelevant, and
// mixed-relevance edges.
func DefaultRerankerDataset() RerankerDataset {
	return RerankerDataset{
		Name: "default-reranker-v1",
		Queries: []RerankerQuery{
			{
				QueryID: "rr-q1",
				Query:   "What language does the user prefer?",
				Candidates: []RankedDoc{
					{DocID: "d-go", Content: "User likes Go", Relevance: 2},
					{DocID: "d-java", Content: "User dislikes Java", Relevance: 1},
					{DocID: "d-weather", Content: "Today is sunny", Relevance: 0},
					{DocID: "d-python", Content: "User uses Python", Relevance: 1},
				},
				IdealOrder: []string{"d-go", "d-java", "d-python", "d-weather"},
			},
			{
				QueryID: "rr-q2",
				Query:   "Where does the user work?",
				Candidates: []RankedDoc{
					{DocID: "d-acme", Content: "User works at Acme Corp", Relevance: 2},
					{DocID: "d-berlin", Content: "User lives in Berlin", Relevance: 1},
					{DocID: "d-vim", Content: "User uses vim", Relevance: 0},
				},
				IdealOrder: []string{"d-acme", "d-berlin", "d-vim"},
			},
			{
				QueryID: "rr-q3",
				Query:   "User's favourite editor",
				Candidates: []RankedDoc{
					{DocID: "d-vim", Content: "User loves vim", Relevance: 2},
					{DocID: "d-emacs", Content: "User tried emacs once", Relevance: 1},
					{DocID: "d-nano", Content: "User has never used nano", Relevance: 0},
				},
				IdealOrder: []string{"d-vim", "d-emacs", "d-nano"},
			},
			// Edge case: all irrelevant
			{
				QueryID: "rr-q4",
				Query:   "User's favourite food",
				Candidates: []RankedDoc{
					{DocID: "d-go", Content: "User likes Go", Relevance: 0},
					{DocID: "d-linux", Content: "User uses Linux", Relevance: 0},
				},
				IdealOrder: []string{"d-go", "d-linux"},
			},
		},
	}
}

// EvaluateReranker scores a reranking function against a dataset
// using NDCG as the primary metric. The rerankFn receives a query
// and candidate doc-ids, and must return them in relevance order.
func EvaluateReranker(dataset RerankerDataset, rerankFn func(query string, candidates []string) ([]string, error)) float64 {
	qrels := make(map[string][]string, len(dataset.Queries))
	results := make(map[string][]string, len(dataset.Queries))

	for _, q := range dataset.Queries {
		// Build qrels entry: all relevant docs (relevance > 0)
		var relDocs []string
		for _, c := range q.Candidates {
			if c.Relevance > 0 {
				relDocs = append(relDocs, c.DocID)
			}
		}
		qrels[q.QueryID] = relDocs

		// Build candidate list
		docIDs := make([]string, len(q.Candidates))
		for i, c := range q.Candidates {
			docIDs[i] = c.DocID
		}

		ranked, err := rerankFn(q.Query, docIDs)
		if err != nil {
			ranked = docIDs // fall back to original order on error
		}
		results[q.QueryID] = ranked
	}

	return NDCG(qrels, results, 10)
}
