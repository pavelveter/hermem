package cmd

import (
	"encoding/json"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
)

func init() { Register("retrieve", cliRetrieve) }

func cliRetrieve(env Env) {
	var req core.RetrieveRequest
	DecodeStdin(&req)
	if len(req.SeedIDs) == 0 {
		log.Fatal("seed_ids required")
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}
	opts := core.RetrieveContextOptions{
		MaxDepth:          req.MaxDepth,
		DepthCeiling:      env.Cfg.MaxDepthCeiling,
		MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
		RankingWeight:     env.Cfg.Ranking,
		Reranker:          env.Reranker,
		Ctx:               env.Ctx,
	}
	result, err := retrieval.RetrieveContext(env.DB, req.SeedIDs, opts)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
