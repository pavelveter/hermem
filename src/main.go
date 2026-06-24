// Package main is now a thin shell — config/DB/vector-index/embedder wiring
// happens here, then a single cmd.Run(name, env) dispatches into the registry
// populated by src/cmd/<name>.go init() functions.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/cmd"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Build-time variables injected via -ldflags:
//
//	-X main.version=$(VERSION)
//	-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
//	-X main.gitCommit=$(git rev-parse --short HEAD)
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `hermem — knowledge graph server and CLI

Usage: hermem <command> [args]

Commands:
  store, search, retrieve, query, response, edge,
                 ingest, explain                  Knowledge CRUD (JSON stdin)
  task-status, task-list, task-show, task-dep,      Task management (JSON stdin)
  task-tree, task-create, task-rollback,
  task-executable (alias: task-next)
  temporal       Temporal retrieval (JSON stdin)
  timeline       List recent entities
  contradictions List contradictions [entity-id]
  agent-loop     Agent execution loop (JSON stdin)
  verify         Graph integrity checker
  migrate, migration-rollback, migration-verify
  execution-plan, recovery-plan, connected-components, communities
  provenance     Query by provenance
  re-embed       Re-embed all entities
  quantize       Quantize an embedding locally (JSON stdin)
  schema         Show schema fingerprint
  health         Health probe (pings DB; mirrors /health/ready)
  metrics        Prometheus exposition (mirrors /metrics)
  serve [port]   Start HTTP server (default :8420)
`)
}

func main() {
	for _, a := range os.Args[1:] {
		if a == "--help" || a == "-h" {
			printUsage(os.Stdout)
			os.Exit(0)
		}
	}
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	cfg, err := config.LoadConfigFromBinaryDir()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.InitDB(config.ResolveDBPath(cfg.DBPath), cfg.VectorDim)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	metrics.InitMetricsDB(db)
	vi := vector.NewIndex(cfg.VectorBackend, db, cfg.VectorDim)
	mw := metrics.InitMetricsWorker(db)
	defer mw.Stop()

	env := cmd.Env{
		Ctx:       context.Background(),
		Cfg:       cfg,
		DB:        db,
		VI:        vi,
		Embedder:  cfg.NewEmbedder(),
		Extractor: cfg.NewExtractor(),
		Reranker:  cfg.NewReranker(),
		Build: cmd.BuildInfo{
			Version:   version,
			BuildDate: buildDate,
			GitCommit: gitCommit,
		},
	}

	if !cmd.Run(os.Args[1], env) {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage(os.Stderr)
		os.Exit(1)
	}
}
