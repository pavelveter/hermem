// Package memory hosts the transport-agnostic MemoryService — the domain
// write/read API for hermem's memory subsystem, POST-PHASE 3.5.
//
// Service carries no transport concerns: no http.ResponseWriter, no metrics
// counters, no SIGHUP hooks, no reference to serverstate.Ref. Handlers (HTTP
// shell in server/memory/, CLI shell in cli/memory/) own all cross-cutting
// plumbing and delegate here for the actual domain work.
//
// After PHASED 3.4 + 3.5 the memory domain is a thin CRUD shell: only
// Store + StoreAndLink remain. The Ingest method moved to
// src/internal/ingest/ (PHASE 3.4), AddEdge moved to src/internal/edge/
// (PHASE 3.5), Timeline + TimelineEntry moved to src/internal/timeline/
// (PHASE 3.5). The four Service fields (db, vi, embedder, extractor)
// are kept for Store + StoreAndLink: db + vi are used by both Store
// and StoreAndLink (via vector.AutoLinkEdges), embedder is used by
// StoreAndLink for AutoLinkEdges, extractor is retained for future
// memory-write hooks (currently unused; the domain Ingest is now in
// src/internal/ingest/ exclusively).
//
// Construction is cheap (six pointer assignments) so callers may instantiate
// fresh per request, but in practice the lifecycle follows the surrounding
// process — main.go builds once via clienv.Env.Service() and both transport
// shells hold a borrowed pointer.
package memory

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service is the transport-agnostic memory domain API.
//
// All methods accept ctx for cancellation propagation and an explicit
// schema arg so the service has no ambient config reads — the daemon
// reload path (SIGHUP) constructs a fresh Service binding against the
// new schema without touching a stateful singleton.
//
// Post-PHASE 3.5 the Service struct field set is the same (db, vi,
// embedder, extractor) even though extractor is no longer called by
// any memory-domain method. Removing the field would force the
// memory constructor signature to drift from the pre-PHASE-3.5
// callers (cli/serve.go + integration_test.go); keeping the field
// holds the door open for a future memory-write extractor hook
// without breaking the constructor boilerplate at every caller.
type Service struct {
	db        *sql.DB
	vi        core.VectorIndex
	embedder  core.Embedder
	extractor core.LLMExtractor
}

// New constructs a Service. All four deps are required; passing a nil
// Extractor used to cause Ingest to fail with "ingest: no extractor
// wired" — pre-PHASE 3.4 / 3.5 contract. Ingest now lives in
// src/internal/ingest/ and the extractor field is retained on memory
// for future memory-write hooks.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor) *Service {
	return &Service{db: db, vi: vi, embedder: embedder, extractor: extractor}
}

// Store persists one entity with its (caller-supplied) embedding.
// Plain domain operation — no AutoLinkEdges side effect.
//
// Use StoreAndLink when you also want the HTTP-only post-store
// related_to auto-discovery (today's `/store` endpoint behaviour).
// CLI /store and future plain consumers should call Store.
//
// Validation precedes persistence: unknown category → ErrInvalidSchema
// with Field="category" so HTTP can map to 422 and CLI can print the
// diagnostic. The DB unique-key constraint and a nil-embedding edge
// case are inherited from store.StoreEntityWithEmbedding.
func (s *Service) Store(ctx context.Context, req core.StoreRequest, schema core.SchemaConfig) error {
	if req.ID == "" || req.Category == "" || req.Content == "" {
		return fmt.Errorf("store: id, category, content required")
	}
	if !schema.AllowedCategories[req.Category] {
		return core.NewInvalidSchemaError("category", req.Category)
	}
	entity := core.Entity{
		ID:        req.ID,
		Category:  req.Category,
		Content:   req.Content,
		Embedding: req.Embedding,
	}
	if err := store.StoreEntityWithEmbedding(s.db, s.vi, schema, entity); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// StoreAndLink is Store followed by vector.AutoLinkEdges. HTTP shell
// calls this for `/store` so the new entity surfaces in the related_to
// graph without a second HTTP hop. CLI /store continues to call plain
// Store (pre-PHASE-2.1 behavior preserved: CLI never auto-linked).
//
// AutoLinkEdges is called with req.Embedding verbatim — no auto-embed
// on empty. This matches pre-PHASE-2.1 HTTP shell behavior exactly:
// whatever embedding the caller supplied (possibly nil) is what
// AutoLinkEdges sees.
func (s *Service) StoreAndLink(ctx context.Context, req core.StoreRequest, schema core.SchemaConfig) error {
	if err := s.Store(ctx, req, schema); err != nil {
		return err
	}
	vector.AutoLinkEdges(ctx, s.db, s.vi, s.embedder, req.ID, req.Embedding) //nolint:errcheck // best-effort: shadow auto-link; not a Save failure
	return nil
}

