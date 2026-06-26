// Package timeline owns the transport-agnostic time-ordered entity
// read API.
//
// PHASE 3.5 lifts Timeline out of memory.Service into its own flat
// pkg following the PHASE 2.x + PHASE 3.1 + 3.2 + 3.3 + 3.4
// precedent: flat pkg, stateless Service, per-call args (limit). No
// HTTP / CLI coupling in the domain pkg. The HTTP shell lives in
// src/internal/server/timeline/.
//
// Timeline is a read-only surface — no embedder + no vector index
// needed. Service struct holds db only; the projection maps the
// entities table schema directly. Where memory.Service used to
// dominate a multi-method API, the new timeline.Service has exactly
// one method (Timeline) plus a TimelineEntry struct.
package timeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Service is the transport-agnostic timeline read API. Read-only — db
// is the only dependency. SQL is plain SELECT with a LIMIT + ORDER BY;
// no transaction.
type Service struct {
	db *sql.DB
}

// New constructs a timeline Service. db is required; nothing else.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// TimelineEntry is one row returned by Timeline. Mirrors the
// projection the HTTP /timeline endpoint has shipped since the schema
// baseline; the pointer-fields use *time.Time / sql.NullString shape
// because hermem has historically treated missing provenance as a
// normal case (extraction pipeline doesn't always populate
// source/conversation/message ids).
//
// The TimelineEntry type MOVED here in PHASE 3.5 from
// src/internal/memory/service.go because it's the natural Timeline
// return type. The transport shell (server/timeline) uses it as
// the in-memory transport-only wire-shape backing struct.
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

// Timeline returns the N most-recently-created entities, archived
// rows filtered out, ordered by created_at DESC. Pagination is
// implicit via the limit arg; the caller (HTTP shell or CLI shell)
// is responsible for any deeper pagination scheme.
//
// mirroring the pre-PHASE-3.5 memory.Service.Timeline SQL verbatim
// so existing /timeline rows surface byte-identical to clients.
func (s *Service) Timeline(ctx context.Context, limit int) ([]TimelineEntry, error) {
	if limit < 0 {
		limit = 0
	}
	// Defensive reference to core.DefaultSchemaConfig(false) pattern:
	// timeline does not validate categories against SchemaConfig because
	// it is a read-only surface (a category must exist in entities
	// before it can be returned). Stored in _ to compile-time check
	// that we haven't accidentally dropped the core import; remove
	// the underscore if a future schema-gated timeline variant lands.
	_ = core.DefaultSchemaConfig

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
	return core.NormalizeSlice(entries), nil
}
