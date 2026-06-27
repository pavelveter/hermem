// Package goal hosts the transport-agnostic GoalService — the domain
// API for goal lifecycle queries. A goal IS a task in hermem's schema,
// distinguished only by Entity.Category == "goal". The domain surface
// is intentionally minimal: Status transitions, List (filtered by
// status/goal-subtree), and Get (single goal by ID).
//
// Service carries no transport concerns: no http.ResponseWriter, no metrics
// counters, no SIGHUP hooks, no reference to serverstate.Ref. Handlers
// (HTTP shell, CLI shell) own all cross-cutting plumbing and delegate
// here for the actual domain work.
//
// One dep — db — matches existing service precedents (graph.Service,
// orchestrator.Service, migration.Service). Construction is one
// pointer assignment so callers may instantiate fresh per request
// or hold a borrowed long-lived pointer.
package goal

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic goal domain API.
//
// All methods accept ctx for cancellation and an explicit schema arg
// so the service has no ambient config reads — the daemon reload path
// (SIGHUP) constructs a fresh Service binding against the new schema
// without touching a stateful singleton.
type Service struct {
	db *sql.DB
}

// New constructs a Goal Service. db is required.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Status transitions a goal's status (e.g. pending → running → completed).
// Delegates to store.SetStatus which enforces category membership and
// valid-state ordering via the schema.
func (s *Service) Status(_ context.Context, id, newStatus string, schema core.SchemaConfig) error {
	if id == "" || newStatus == "" {
		return fmt.Errorf("goal: Status: id and new status required")
	}
	if err := store.SetStatus(s.db, schema, id, newStatus); err != nil {
		return fmt.Errorf("goal: Status: %w", err)
	}
	return nil
}

// List returns goal entities filtered by optional status and/or goal
// subtree root. Empty filters mean "no filter on that dimension".
// Nil→empty slice normalization promotes the downstream envelope
// contract (JSON `[]` not `null`).
func (s *Service) List(_ context.Context, status, goalID string, schema core.SchemaConfig) ([]core.Task, error) {
	return store.ListTasks(s.db, schema, status, goalID)
}

// Get returns a single goal entity by ID. Returns an error wrapping
// the store-level "task not found" message so callers can
// errors.Is-check the sentinel if needed.
func (s *Service) Get(_ context.Context, id string, schema core.SchemaConfig) (core.Task, error) {
	if id == "" {
		return core.Task{}, fmt.Errorf("goal: Get: id required")
	}
	t, err := store.GetTaskByID(s.db, schema, id)
	if err != nil {
		return core.Task{}, fmt.Errorf("goal: Get: %w", err)
	}
	return t, nil
}
