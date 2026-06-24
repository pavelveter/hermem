package cmd

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func init() { Register("temporal", cliTemporal) }

func cliTemporal(env Env) {
	var req struct {
		Query    string `json:"query"`
		TimeFrom string `json:"time_from"`
		TimeTo   string `json:"time_to"`
		TopK     int    `json:"top_k"`
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
		RankingWeight:     env.Cfg.Ranking,
		Reranker:          env.Reranker,
	}
	if req.TimeFrom != "" {
		if t, err := time.Parse(time.RFC3339, req.TimeFrom); err == nil {
			opts.TimeFrom = t
		}
	}
	if req.TimeTo != "" {
		if t, err := time.Parse(time.RFC3339, req.TimeTo); err == nil {
			opts.TimeTo = t
		}
	}
	result, err := retrieval.RetrieveContext(env.DB, seedIDs, opts)
	if err != nil {
		log.Fatalf("temporal: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
