package evaluation

// Precision computes Precision@K: the fraction of top-K results that are
// relevant, averaged across all queries.
//
// qrels maps query-id → list of relevant document-ids.
// results maps query-id → ranked list of retrieved document-ids (truncated to K).
//
// Returns 0 when qrels is empty.
func Precision(qrels map[string][]string, results map[string][]string, k int) float64 {
	if len(qrels) == 0 || k <= 0 {
		return 0
	}

	totalRelevant := 0
	totalRetrieved := 0

	for qid, rel := range qrels {
		relSet := make(map[string]struct{}, len(rel))
		for _, id := range rel {
			relSet[id] = struct{}{}
		}

		rets := results[qid]
		if k < len(rets) {
			rets = rets[:k]
		}

		for _, id := range rets {
			totalRetrieved++
			if _, ok := relSet[id]; ok {
				totalRelevant++
			}
		}
	}

	if totalRetrieved == 0 {
		return 0
	}
	return float64(totalRelevant) / float64(totalRetrieved)
}
