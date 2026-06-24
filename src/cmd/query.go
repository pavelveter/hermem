package cmd

import (
	"encoding/json"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func init() { Register("query", cliQuery) }

func cliQuery(env Env) {
	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k,omitempty"`
	}
	DecodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}
	emb, _ := env.Embedder.Embed(env.Ctx, req.Query)
	results, _ := vector.SearchByVector(env.DB, env.VI, emb, req.TopK)
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{
		MaxDepth:          2,
		DepthCeiling:      env.Cfg.MaxDepthCeiling,
		MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
		QueryEmbedding:    emb,
		QueryText:         req.Query,
		Ctx:               env.Ctx,
		RankingWeight:     env.Cfg.Ranking,
		Reranker:          env.Reranker,
	}
	ctxResult, err := retrieval.RetrieveContext(env.DB, seedIDs, opts)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
		"context": retrieval.FormatContextMarkdown(ctxResult),
	})
}
