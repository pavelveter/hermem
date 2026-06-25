package evaluation

// MRR computes Mean Reciprocal Rank: the average of 1/rank for the first
// relevant document in each query's result list.
//
// qrels maps query-id → list of relevant document-ids.
// results maps query-id → ranked list of retrieved document-ids.
//
// Returns 0 when qrels is empty.
func MRR(qrels map[string][]string, results map[string][]string) float64 {
	if len(qrels) == 0 {
		return 0
	}

	sum := 0.0
	queried := 0

	for qid, rel := range qrels {
		if len(rel) == 0 {
			continue
		}
		queried++

		relSet := make(map[string]struct{}, len(rel))
		for _, id := range rel {
			relSet[id] = struct{}{}
		}

		rets := results[qid]
		for rank, id := range rets {
			if _, ok := relSet[id]; ok {
				sum += 1.0 / float64(rank+1)
				break
			}
		}
	}

	if queried == 0 {
		return 0
	}
	return sum / float64(queried)
}
