// Package memory hosts the transport-agnostic MemoryService — the domain
// write/read API for hermem's memory subsystem.
//
// Service carries no transport concerns: no http.ResponseWriter, no metrics
// counters, no SIGHUP hooks, no reference to serverstate.Ref. Handlers (HTTP
// shell in server/memory/, CLI shell in cli/memory/) own all cross-cutting
// plumbing and delegate here for the actual domain work.
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
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service is the transport-agnostic memory domain API.
//
// All methods accept ctx for cancellation propagation and an explicit
// schema arg so the service has no ambient config reads — the daemon
// reload path (SIGHUP) constructs a fresh Service binding against the
// new schema without touching a stateful singleton.
type Service struct {
	db        *sql.DB
	vi        core.VectorIndex
	embedder  core.Embedder
	extractor core.LLMExtractor
}

// New constructs a Service. All four deps are required; passing a nil
// Extractor causes Ingest to fail with "ingest: no extractor wired".
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor) *Service {
	return &Service{db: db, vi: vi, embedder: embedder, extractor: extractor}
}

// TimelineEntry is one row returned by Timeline. Mirrors the projection
// the HTTP /timeline endpoint has shipped since the schema baseline; the
// pointer-fields use *time.Time / sql.NullString shape because hermem has
// historically treated missing provenance as a normal case (extraction
// pipeline doesn't always populate source/conversation/message ids).
type TimelineEntry struct {
	ID             string
	Category       string
	Content        string
	CreatedAt      *time.Time
	Source         string
	SourceType     string
	ConversationID string
	MessageID      string
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
		return &ErrInvalidSchema{Field: "category", Value: req.Category}
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
	vector.AutoLinkEdges(ctx, s.db, s.vi, s.embedder, req.ID, req.Embedding)
	return nil
}

// Ingest runs LLM extraction → embed → DB-insert on one dialog.
//
// IngestionWorker is constructed PER CALL (six pointer assignments;
// cheap) rather than held as a long-lived Service field. Two reasons:
//
//  1. SIGHUP race — the long-lived pre-PHASE-2.1 worker mutates
//     schema mid-call via Worker.ReloadSchema; per-call construction
//     binds the schema at call time so reloaded-on-different-state
//     scenarios are unaffected by goroutine-local mutation races.
//
//  2. CLI/HTP parity — both transports end up running identical
//     pipeline code through a freshly-constructed worker; no
//     "production-only" / "CLI-only" divergence.
func (s *Service) Ingest(ctx context.Context, dialog string, dedupThreshold float32, schema core.SchemaConfig) error {
	if dialog == "" {
		return fmt.Errorf("ingest: dialog required")
	}
	if s.extractor == nil {
		return fmt.Errorf("ingest: no extractor wired")
	}
	w := ingestion.NewIngestionWorker(s.db, s.vi, s.extractor, s.embedder, dedupThreshold, schema)
	if err := w.ProcessDialog(ctx, dialog); err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return nil
}

// AddEdge persists a relation edge, optionally auto-creating missing
// endpoint entities via vector.AddEdgeWithAutoCreate.
//
// Validation precedes dispatch: unknown relation_type → ErrInvalidSchema
// with Field="relation_type". Both branches (auto_create true/false)
// resolve through SchemaConfig.AllowedRelations before touching the DB
// so a malformed request cannot bypass write-side guards.
func (s *Service) AddEdge(ctx context.Context, req core.EdgeRequest, schema core.SchemaConfig) error {
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		return fmt.Errorf("edge: source_id, target_id, relation_type required")
	}
	if !schema.AllowedRelations[req.RelationType] {
		return &ErrInvalidSchema{Field: "relation_type", Value: req.RelationType}
	}
	if req.AutoCreate {
		if err := vector.AddEdgeWithAutoCreate(ctx, s.db, s.vi, s.embedder, req.SourceID, req.TargetID, req.RelationType); err != nil {
			return fmt.Errorf("edge auto-create: %w", err)
		}
		return nil
	}
	if err := store.AddEdge(s.db, req.SourceID, req.TargetID, req.RelationType, req.Weight); err != nil {
		return fmt.Errorf("edge: %w", err)
	}
	return nil
}

// Timeline returns the N most-recently-created entities, archived rows
// filtered out, ordered by created_at DESC. Pagination is implicit via
// the limit arg; the caller (HTTP shell or CLI shell) is responsible for
// any deeper pagination scheme.
func (s *Service) Timeline(ctx context.Context, limit int) ([]TimelineEntry, error) {
	if limit < 0 {
		limit = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, category, content, created_at, source, source_type, conversation_id, message_id FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("timeline query: %w", err)
	}
	defer rows.Close()
	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var createdAt sql.NullTime
		var source, sourceType, convID, msgID sql.NullString
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &createdAt, &source, &sourceType, &convID, &msgID); err != nil {
			return nil, fmt.Errorf("timeline scan: %w", err)
		}
		if createdAt.Valid {
			t := createdAt.Time
			e.CreatedAt = &t
		}
		if source.Valid {
			e.Source = source.String
		}
		if sourceType.Valid {
			e.SourceType = sourceType.String
		}
		if convID.Valid {
			e.ConversationID = convID.String
		}
		if msgID.Valid {
			e.MessageID = msgID.String
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("timeline rows: %w", err)
	}
	if entries == nil {
		entries = []TimelineEntry{}
	}
	return entries, nil
}

// ErrInvalidSchema is returned by Service methods when a request violates
// the supplied schema. HTTP shell maps it to 422 Unprocessable Entity;
// CLI shell prints the message verbatim.
//
// Field values are "category" and "relation_type" — those are the two
// schema-validated surfaces a memory write touches. Value carries the
// offending literal for the operator's diagnostic.
type ErrInvalidSchema struct {
	Field string
	Value string
}

func (e *ErrInvalidSchema) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Value)
}
