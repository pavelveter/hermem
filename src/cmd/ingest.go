package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
)

func init() { Register("ingest", cliIngest) }

func cliIngest(env Env) {
	var req core.IngestRequest
	DecodeStdin(&req)
	if req.Dialog == "" {
		log.Fatal("dialog required")
	}
	w := ingestion.NewIngestionWorker(env.DB, env.VI, env.Extractor, env.Embedder, env.Cfg.DedupThreshold, env.Cfg.Schema)
	if err := w.ProcessDialog(env.Ctx, req.Dialog); err != nil {
		log.Fatalf("ingest: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}
