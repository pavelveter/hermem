package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
)

// MultiHopRetrieveContext performs iterative graph expansion: hops of
// vector search → graph walk → discover new seeds from top facts →
// repeat. After the final hop, the reranker (if configured) fires once.
//
// Hop 1: call RetrieveContext with the original seed IDs.
// Hop N: take the top-K facts by ranking score from hop N-1, embed
// each fact's content as a query, vector search for each, collect
// unique new seed IDs (not already visited), call RetrieveContext
// again with those seeds. Merge results.
//
// MultiHopCount controls the number of search→expand cycles.
// 0 or 1 = single hop (delegates to RetrieveContext directly).
// 2+ = multi-hop.
func MultiHopRetrieveContext(
	db *sql.DB,
	vi VectorIndex,
	embedder Embedder,
	seedIDs []string,
	opts RetrieveContextOptions,
) (*RetrievalResult, error) {
	if opts.MultiHopCount <= 1 {
		return RetrieveContext(db, seedIDs, opts)
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Hop 1: initial graph walk from seed IDs.
	result, err := RetrieveContext(db, seedIDs, opts)
	if err != nil {
		return nil, fmt.Errorf("multi-hop hop 1: %w", err)
	}

	// Collect all content seen so far for dedup across hops.
	seenContent := make(map[string]bool)
	collectContent := func(r *RetrievalResult) {
		for _, f := range r.SeedNodes {
			seenContent[f.Entity.Content] = true
		}
		for _, f := range r.WorldFacts {
			seenContent[f.Content] = true
		}
		for _, f := range r.Opinions {
			seenContent[f.Content] = true
		}
		for _, f := range r.Experiences {
			seenContent[f.Content] = true
		}
		for _, f := range r.Observations {
			seenContent[f.Content] = true
		}
	}
	collectContent(result)

	// Top K facts per hop to re-expand from.
	const topK = 3

	for hop := 2; hop <= opts.MultiHopCount; hop++ {
		// Extract top-K facts (by ranking score) from the previous result.
		queries := topFactsAsQueries(result, topK, seenContent)
		if len(queries) == 0 {
			slog.Debug("multi-hop: no new facts to expand from",
				"event", "multi_hop_empty", "hop", hop)
			break
		}

		// Embed each fact's content and do vector search.
		var newSeeds []string
		seenIDs := make(map[string]bool)
		for _, s := range seedIDs {
			seenIDs[s] = true
		}
		// Also avoid re-visiting entities already in the result.
		for _, n := range result.SeedNodes {
			seenIDs[n.Entity.ID] = true
		}

		// Embed all queries in parallel, then SearchBatch for single lock.
		var queryEmbs [][]float32
		for _, q := range queries {
			emb, err := embedder.Embed(ctx, q)
			if err != nil {
				slog.Warn("multi-hop: embed failed", "event", "multi_hop_embed_err",
					"hop", hop, "query", truncate(q, 40), "err", err)
				continue
			}
			queryEmbs = append(queryEmbs, emb)
		}

		if len(queryEmbs) > 0 {
			allIDs, err := vi.SearchBatch(ctx, queryEmbs, 5)
			if err != nil {
				slog.Warn("multi-hop: batch search failed", "event", "multi_hop_search_err",
					"hop", hop, "err", err)
			} else {
				for _, ids := range allIDs {
					for _, id := range ids {
						if !seenIDs[id] {
							seenIDs[id] = true
							newSeeds = append(newSeeds, id)
						}
					}
				}
			}
		}

		if len(newSeeds) == 0 {
			slog.Debug("multi-hop: no new seeds discovered",
				"event", "multi_hop_no_seeds", "hop", hop)
			break
		}

		// Hop N: graph walk from newly discovered seeds.
		hopResult, err := RetrieveContext(db, newSeeds, opts)
		if err != nil {
			return nil, fmt.Errorf("multi-hop hop %d: %w", hop, err)
		}

		// Merge hopResult into result, deduping by content.
		mergeResults(result, hopResult, seenContent)
		collectContent(hopResult)

		slog.Debug("multi-hop expansion complete",
			"event", "multi_hop_expand",
			"hop", hop,
			"new_seeds", len(newSeeds),
			"new_facts", countFacts(hopResult),
		)
	}

	// Reranker fires once after all hops (already done inside
	// RetrieveContext for each hop individually; this is acceptable
	// since each hop's bucket is reranked locally).

	return result, nil
}

// topFactsAsQueries extracts the top-K unique fact contents (by ranking
// score) from a RetrievalResult, skipping anything already in seenContent.
func topFactsAsQueries(result *RetrievalResult, k int, seenContent map[string]bool) []string {
	type scored struct {
		content string
		score   float32
	}
	var facts []scored

	addIfNew := func(content string, score float32) {
		if seenContent[content] || content == "" {
			return
		}
		facts = append(facts, scored{content, score})
	}

	for _, n := range result.SeedNodes {
		addIfNew(n.Entity.Content, n.RankingScore)
	}
	for _, f := range result.WorldFacts {
		addIfNew(f.Content, f.RankingScore)
	}
	for _, f := range result.Opinions {
		addIfNew(f.Content, f.RankingScore)
	}
	for _, f := range result.Experiences {
		addIfNew(f.Content, f.RankingScore)
	}
	for _, f := range result.Observations {
		addIfNew(f.Content, f.RankingScore)
	}

	// Sort descending by score, then by content for determinism.
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].score != facts[j].score {
			return facts[i].score > facts[j].score
		}
		return facts[i].content < facts[j].content
	})

	if len(facts) > k {
		facts = facts[:k]
	}

	var queries []string
	for _, f := range facts {
		queries = append(queries, f.content)
	}
	return queries
}

// mergeResults appends hopResult's facts into result, skipping duplicates
// already in seenContent. seenContent is updated in-place.
// SeedNodes are merged as well — hop N may discover new seed-level entities.
func mergeResults(result, hopResult *RetrievalResult, seenContent map[string]bool) {
	for _, n := range hopResult.SeedNodes {
		if !seenContent[n.Entity.Content] {
			seenContent[n.Entity.Content] = true
			result.SeedNodes = append(result.SeedNodes, n)
		}
	}

	mergeFacts := func(dst *[]RetrievedFact, src []RetrievedFact) {
		for _, f := range src {
			if !seenContent[f.Content] {
				seenContent[f.Content] = true
				*dst = append(*dst, f)
			}
		}
	}

	mergeFacts(&result.WorldFacts, hopResult.WorldFacts)
	mergeFacts(&result.Opinions, hopResult.Opinions)
	mergeFacts(&result.Experiences, hopResult.Experiences)
	mergeFacts(&result.Observations, hopResult.Observations)
}

func countFacts(r *RetrievalResult) int {
	return len(r.WorldFacts) + len(r.Opinions) + len(r.Experiences) + len(r.Observations)
}
