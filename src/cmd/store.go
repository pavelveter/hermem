package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("store", cliStore) }

func cliStore(env Env) {
	var req core.StoreRequest
	DecodeStdin(&req)
	if req.ID == "" || req.Category == "" || req.Content == "" {
		log.Fatal("id, category, content required")
	}
	if err := env.Cfg.ValidateCategory(req.Category); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if len(req.Embedding) == 0 {
		emb, err := env.Embedder.Embed(env.Ctx, req.Content)
		if err != nil {
			log.Fatalf("embed: %v", err)
		}
		req.Embedding = emb
	}
	if err := store.StoreEntityWithEmbedding(env.DB, env.VI, env.Cfg.Schema, core.Entity{
		ID:        req.ID,
		Category:  req.Category,
		Content:   req.Content,
		Embedding: req.Embedding,
	}); err != nil {
		log.Fatalf("store: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}
