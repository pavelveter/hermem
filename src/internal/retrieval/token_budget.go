package retrieval

import "github.com/pavelveter/hermem/src/internal/core"

// TrimByTokenBudget trims a RetrievalResult so that the total estimated
// token count stays within the budget. Facts are trimmed per-bucket
// (world, opinion, experience, observation) starting from the lowest-scored
// (last in each bucket, since buckets are sorted by ranking score DESC).
// If budget is 0, the result is returned unchanged.
func TrimByTokenBudget(result *core.RetrievalResult, budget int) *core.RetrievalResult {
	if result == nil || budget <= 0 {
		return result
	}

	// Estimate current token count.
	used := estimateResultTokens(result)
	if used <= budget {
		return result
	}

	// Trim from each bucket proportionally, starting with the largest.
	type bucket struct {
		name  string
		facts *[]core.RetrievedFact
	}
	buckets := []bucket{
		{"world", &result.WorldFacts},
		{"opinion", &result.Opinions},
		{"experience", &result.Experiences},
		{"observation", &result.Observations},
	}

	remaining := budget
	for _, b := range buckets {
		if remaining <= 0 {
			*b.facts = nil
			continue
		}
		bucketTokens := estimateFactsTokens(*b.facts)
		if bucketTokens <= remaining {
			remaining -= bucketTokens
			continue
		}
		// Trim this bucket to fit within remaining budget.
		*b.facts = trimFactsToBudget(*b.facts, remaining)
		remaining = 0
	}

	return result
}

func estimateResultTokens(r *core.RetrievalResult) int {
	total := 0
	total += estimateFactsTokens(r.WorldFacts)
	total += estimateFactsTokens(r.Opinions)
	total += estimateFactsTokens(r.Experiences)
	total += estimateFactsTokens(r.Observations)
	return total
}

func estimateFactsTokens(facts []core.RetrievedFact) int {
	total := 0
	for _, f := range facts {
		total += CountTokens(f.Content) + 2 // +2 for "- " prefix and "\n"
	}
	return total
}

// trimFactsToBudget keeps facts from the start (highest-scored) until
// the token budget is exhausted. Fact ordering in each bucket is already
// sorted by ranking score DESC from scoreAndRank.
func trimFactsToBudget(facts []core.RetrievedFact, budget int) []core.RetrievedFact {
	used := 0
	for i, f := range facts {
		cost := CountTokens(f.Content) + 2
		if used+cost > budget {
			return facts[:i]
		}
		used += cost
	}
	return facts
}
