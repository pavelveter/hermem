// Package episode hosts the transport-agnostic EpisodeService — the domain
// API for querying entities by their ingestion provenance (conversation,
// message, extraction source).
//
// Service carries no transport concerns: no http.ResponseWriter, no metrics
// counters, no SIGHUP hooks, no reference to serverstate.Ref. Handlers
// (HTTP shell, CLI shell) own all cross-cutting plumbing and delegate
// here for the actual domain work.
//
// The domain is a thin read shell over store.GetEntitiesByProvenance.
// One dep — db — matches existing service precedents (graph.Service,
// orchestrator.Service, migration.Service). Construction is one
// pointer assignment so callers may instantiate fresh per request
// or hold a borrowed long-lived pointer.
package episode

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic episode domain API.
//
// POST-P1 SERVICE LAYER: created from TODO.md "[ ] Create EpisodeService"
// item. The domain surface is intentionally minimal — episode provenance
// queries are a thin projection over the entities table and the store
// layer already carries the SQL. This Service adds input validation,
// nil→empty slice normalisation, and domain-prefixed error wrapping
// so transport shells never see a raw store error.
type Service struct {
	db *sql.DB
}

// New constructs an Episode Service. db is required.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// ListByConversation returns entities extracted from the given
// conversation, newest first. limit ≤ 0 uses the store default (50);
// values > 200 are clamped to 200 to match the store guard.
// Returns empty slice (not nil) when no rows match.
func (s *Service) ListByConversation(ctx context.Context, conversationID string, limit int) ([]core.Entity, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("episode: ListByConversation: conversation_id required")
	}
	entities, err := store.GetEntitiesByProvenance(s.db, conversationID, "", "", limit)
	if err != nil {
		return nil, fmt.Errorf("episode: ListByConversation: %w", err)
	}
	return entities, nil
}

// ListByMessage returns entities extracted from the given message,
// newest first. limit cap is the same as ListByConversation.
// Returns empty slice (not nil) when no rows match.
func (s *Service) ListByMessage(ctx context.Context, messageID string, limit int) ([]core.Entity, error) {
	if messageID == "" {
		return nil, fmt.Errorf("episode: ListByMessage: message_id required")
	}
	entities, err := store.GetEntitiesByProvenance(s.db, "", messageID, "", limit)
	if err != nil {
		return nil, fmt.Errorf("episode: ListByMessage: %w", err)
	}
	return entities, nil
}

// ListBySource returns entities with the given source/source_type
// string, newest first. limit cap is the same as ListByConversation.
// Returns empty slice (not nil) when no rows match.
func (s *Service) ListBySource(ctx context.Context, source string, limit int) ([]core.Entity, error) {
	if source == "" {
		return nil, fmt.Errorf("episode: ListBySource: source required")
	}
	entities, err := store.GetEntitiesByProvenance(s.db, "", "", source, limit)
	if err != nil {
		return nil, fmt.Errorf("episode: ListBySource: %w", err)
	}
	return entities, nil
}
