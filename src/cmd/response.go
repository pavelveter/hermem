package cmd

import (
	"encoding/json"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
)

func init() { Register("response", cliResponse) }

func cliResponse(env Env) {
	var req struct {
		Query    string `json:"query"`
		MaxDepth int    `json:"max_depth,omitempty"`
	}
	DecodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query is required")
	}
	opts := core.RetrieveContextOptions{
		DepthCeiling:      env.Cfg.MaxDepthCeiling,
		MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
		RankingWeight:     env.Cfg.Ranking,
		Reranker:          env.Reranker,
	}
	if req.MaxDepth > 0 {
		opts.MaxDepth = req.MaxDepth
	}
	out, err := retrieval.GenerateResponse(env.Ctx, env.DB, env.VI, env.Embedder, opts, req.Query)
	if err != nil {
		log.Fatalf("response: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"response": out})
}
