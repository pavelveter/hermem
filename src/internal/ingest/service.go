// Package ingest owns the transport-agnostic ingest orchestrator.
//
// lifts the synchronous Ingest method out of memory.Service
// and into its own flat pkg following the PHASE 2.x + +
// 3.3 precedent: stateless Service, per-call args for things that change
// request-time (schema, dedupThreshold), and no HTTP / CLI coupling in
// the domain pkg.
//
// Construction is cheap (six pointer assignments) so callers may
// instantiate fresh per request, but in practice the lifecycle follows
// the surrounding process — cli/serve.go builds once via clienv.Env
// and the server/ingest HTTP shell holds a borrowed pointer.
//
// The heavy-lifting pipeline (extraction → embed → dedup → upsert →
// edges) lives in src/internal/ingestion/IngestionWorker which this
// Service delegates to. The double naming is intentional:
//
// ingestion/ owns the algorithm (the 600 LOC of pipeline);
// ingest/ owns the transport-agnostic orchestration shell that
// wraps one IngestionWorker invocation per call.
package ingest

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/ingestion/detectors"
)

// Service is the transport-agnostic ingest orchestrator.
//
// Mirrors memory.Service field shape: db + vi + embedder
// + extractor. ownership split is in the *caller* layer —
// memory.Service no longer embeds these fields once Ingest moves out;
// memory.Service.StoreAndLink continues to use them for the write/read
// surface, ingest.Service.Ingest uses them for the dialog pipeline.
type Service struct {
	db        *sql.DB
	vi        core.VectorIndex
	embedder  core.Embedder
	extractor core.LLMExtractor
}

// NewService constructs a Service. All four deps are required; passing
// a nil Extractor causes Ingest to fail with "ingest: no extractor
// wired" — same contract as the memory.Service.Ingest.
//
// Parameter order matches memory.Service.New so callers (cli + HTTP
// fixture) can keep their reference shape: db, vi, embedder, extractor.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor) *Service {
	return &Service{db: db, vi: vi, embedder: embedder, extractor: extractor}
}

// Ingest runs LLM extraction → embed → DB-insert on one dialog.
//
// IngestionWorker is constructed PER CALL (six pointer assignments;
// cheap) rather than held as a long-lived Service field. Two reasons:
// 1. SIGHUP race — the long-lived worker mutates
// schema mid-call via Worker.ReloadSchema; per-call construction
// binds dedupThreshold + schema at call time so reloaded-during-
// call scenarios are unaffected by goroutine-local mutation races.
// 2. CLI/HTTP parity — both transports end up running identical
// pipeline code through a freshly-constructed worker; no
// "production-only" / "CLI-only" divergence.
//
// Per-call `dedupThreshold` + `schema` mirror
// memory.Service.Ingest signature verbatim so transport shells
// forwarding the call (server/ingest + cli/memory/ingest) keep their
// call shape — they just call ingest.Service.Ingest instead of
// memory.Service.Ingest.
func (s *Service) Ingest(ctx context.Context, dialog string, dedupThreshold float32, schema core.SchemaConfig) error {
	if dialog == "" {
		return fmt.Errorf("ingest: dialog required")
	}
	if s.extractor == nil {
		return fmt.Errorf("ingest: no extractor wired")
	}
	// Pass an explicit lexical detector so future wiring can substitute a
	// detectors.NewCompositeDetector(detectors.NewLexicalDetector(), detectors.NewSemanticDetector())
	// at this single call site without changing the worker contract.
	w := ingestion.NewIngestionWorker(s.db, s.vi, s.extractor, s.embedder, dedupThreshold, schema, detectors.NewLexicalDetector())
	if err := w.ProcessDialog(ctx, dialog); err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return nil
}
