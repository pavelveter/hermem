package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func init() { Register("edge", cliEdge) }

func cliEdge(env Env) {
	var req struct {
		SourceID     string  `json:"source_id"`
		TargetID     string  `json:"target_id"`
		RelationType string  `json:"relation_type"`
		Weight       float32 `json:"weight,omitempty"`
		AutoCreate   bool    `json:"auto_create,omitempty"`
	}
	DecodeStdin(&req)
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		log.Fatal("source_id, target_id, relation_type required")
	}
	if err := env.Cfg.ValidateRelation(req.RelationType); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	var edgeErr error
	if req.AutoCreate {
		edgeErr = vector.AddEdgeWithAutoCreate(env.Ctx, env.DB, env.VI, env.Embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		edgeErr = store.AddEdge(env.DB, req.SourceID, req.TargetID, req.RelationType, req.Weight)
	}
	if edgeErr != nil {
		log.Fatalf("edge: %v", edgeErr)
	}
	fmt.Println(`{"status":"ok"}`)
}
