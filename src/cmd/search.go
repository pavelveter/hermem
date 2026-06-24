package cmd

import (
	"encoding/json"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/vector"
)

func init() { Register("search", cliSearch) }

func cliSearch(env Env) {
	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k,omitempty"`
	}
	DecodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	emb, err := env.Embedder.Embed(env.Ctx, req.Query)
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	results, err := vector.SearchByVector(env.DB, env.VI, emb, req.TopK)
	if err != nil {
		log.Fatalf("search: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(results)
}
