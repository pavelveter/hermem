package evaluation

// Recall computes Recall@K: the fraction of relevant documents that appear
// in the top-K results across all queries.
//
// qrels maps query-id → list of relevant document-ids.
// results maps query-id → ranked list of retrieved document-ids (truncated to K).
//
// Returns 0 when qrels is empty.
func Recall(qrels map[string][]string, results map[string][]string, k int) float64 {
	if len(qrels) == 0 || k <= 0 {
		return 0
	}

	totalRelevant := 0
	totalFound := 0

	for qid, rel := range qrels {
		if len(rel) == 0 {
			continue
		}
		totalRelevant += len(rel)

		relSet := make(map[string]struct{}, len(rel))
		for _, id := range rel {
			relSet[id] = struct{}{}
		}

		rets := results[qid]
		if k < len(rets) {
			rets = rets[:k]
		}
		for _, id := range rets {
			if _, ok := relSet[id]; ok {
				totalFound++
			}
		}
	}

	if totalRelevant == 0 {
		return 0
	}
	return float64(totalFound) / float64(totalRelevant)
}
