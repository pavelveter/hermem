// Package memory hosts the transport-agnostic MemoryService — the domain
// write/read API for hermem's memory subsystem, .
//
// Service carries no transport concerns: no http.ResponseWriter, no metrics
// counters, no SIGHUP hooks, no reference to serverstate.Ref. Handlers (HTTP
// shell in server/memory/, CLI shell in cli/memory/) own all cross-cutting
// plumbing and delegate here for the actual domain work.
//
// After the memory domain is a thin CRUD shell: only
// Store + StoreAndLink remain. The Ingest method moved to
// src/internal/ingest/ (), AddEdge moved to src/internal/edge/
// (), Timeline + TimelineEntry moved to src/internal/timeline/
// (). The three Service fields (db, vi, embedder) cover
// Store + StoreAndLink: db + vi persist the row + write to the vector
// index, embedder is used by StoreAndLink's vector.AutoLinkEdges for
// the related_to auto-discovery path. The LLM extractor is no longer
// threaded through here — the dialog-pipeline extractor wiring lives
// in src/internal/ingest/, where it's actually consumed.
//
// Construction is cheap (three pointer assignments) so callers may instantiate
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
// the Service is a thin CRUD shell: only Store +
// StoreAndLink remain. Ingest moved to src/internal/ingest/ (PHASE
// 3.4), AddEdge moved to src/internal/edge/ (), Timeline
// moved to src/internal/timeline/ (). The remaining three
// Service fields (db, vi, embedder) are all used by Store + StoreAndLink:
// db + vi persist the row + write to the vector index, embedder is
// used by StoreAndLink's vector.AutoLinkEdges for the related_to
// auto-discovery path. The LLM extractor is now owned exclusively
// by src/internal/ingest/ — passing it here would be dead weight
// (no memory-domain method calls it ).
type Service struct {
	db       *sql.DB
	vi       core.VectorIndex
	embedder core.Embedder
}

// New constructs a Service. db + vi + embedder are the only deps
// needed for the surface (Store + StoreAndLink).
// The LLM extractor is no longer threaded through here — the
// dialog-pipeline extractor wiring lives in src/internal/ingest/,
// where it's actually consumed.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder) *Service {
	return &Service{db: db, vi: vi, embedder: embedder}
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
// Store ( behavior preserved: CLI never auto-linked).
//
// AutoLinkEdges is called with req.Embedding verbatim — no auto-embed
// on empty. This matches HTTP shell behavior exactly:
// whatever embedding the caller supplied (possibly nil) is what
// AutoLinkEdges sees.
func (s *Service) StoreAndLink(ctx context.Context, req core.StoreRequest, schema core.SchemaConfig) error {
	if err := s.Store(ctx, req, schema); err != nil {
		return err
	}
	vector.AutoLinkEdges(ctx, s.db, s.vi, s.embedder, req.ID, req.Embedding) //nolint:errcheck // shadow auto-link; not a Save failure
	return nil
}
