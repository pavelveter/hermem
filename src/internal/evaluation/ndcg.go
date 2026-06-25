package evaluation

import (
	"math"
)

// NDCG computes Normalized Discounted Cumulative Gain at K.
// Uses binary relevance (relevant=1, not relevant=0).
//
// qrels maps query-id → list of relevant document-ids.
// results maps query-id → ranked list of retrieved document-ids (truncated to K).
//
// Returns 0 when qrels is empty or no relevant docs exist.
func NDCG(qrels map[string][]string, results map[string][]string, k int) float64 {
	if len(qrels) == 0 || k <= 0 {
		return 0
	}

	sum := 0.0
	queried := 0

	for qid, rel := range qrels {
		if len(rel) == 0 {
			continue
		}

		relSet := make(map[string]struct{}, len(rel))
		for _, id := range rel {
			relSet[id] = struct{}{}
		}

		// DCG: gain from actual results
		dcg := 0.0
		rets := results[qid]
		if k < len(rets) {
			rets = rets[:k]
		}
		for i, id := range rets {
			if _, ok := relSet[id]; ok {
				dcg += 1.0 / math.Log2(float64(i+2)) // i+2 because log2(1) = 0
			}
		}

		// IDCG: ideal DCG — all relevant docs ranked first
		idealCount := len(rel)
		if idealCount > k {
			idealCount = k
		}
		idcg := 0.0
		for i := 0; i < idealCount; i++ {
			idcg += 1.0 / math.Log2(float64(i+2))
		}

		if idcg > 0 {
			sum += dcg / idcg
			queried++
		}
	}

	if queried == 0 {
		return 0
	}
	return sum / float64(queried)
}
