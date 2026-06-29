// Package task hosts the transport-agnostic TaskService — the domain
// API for hermem's task subsystem: state transitions, listing,
// show-with-relations, dependency edges, rollback lookup, tree
// rendering, recovery-plan suggestion, and task creation with the
// embed+store+link side effect.
package task

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service is the transport-agnostic task domain API.
//
// Three deps, all required, all passed by pointer at construction time:
//   - db       : *sql.DB      — every task store fn hits SQL
//   - embedder : core.Embedder — Create embeds new task content
//   - vi       : core.VectorIndex — Create + AutoLinkEdges need to write
//     the task's embedding into the cosine index so the
//     related_to auto-discovery links it to neighbours
//
// No Refs field, no Metrics field — the HTTP shell loads schema per
// request and increments counters per-response; the domain never
// reaches into either.
type Service struct {
	db       *sql.DB
	embedder core.Embedder
	vi       core.VectorIndex
}

// New constructs a Service. All three deps are required; passing
// nil embedder makes Create fail with a domain error that the HTTP
// shell maps to 500.
func New(db *sql.DB, embedder core.Embedder, vi core.VectorIndex) *Service {
	return &Service{db: db, embedder: embedder, vi: vi}
}

// Status transitions a task's status (e.g. pending → running → completed).
// Validation is delegated to store.SetStatus.
//
// Errors: returns core.NewNotFoundError if the entity doesn't exist;
// other store errors (invalid status, non-stateful) are wrapped as-is
// and map to 422 in the HTTP shell via MapError's default→500 codepath
// plus a task-specific override in HandleTaskStatus.
func (s *Service) Status(_ context.Context, id, newStatus string, schema core.SchemaConfig) error {
	if id == "" || newStatus == "" {
		return core.NewInvalidInputError("id and new status required")
	}
	if err := store.SetStatus(s.db, schema, id, newStatus); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return core.NewNotFoundError(err.Error())
		}
		return fmt.Errorf("status: %w", err)
	}
	return nil
}

// Executable returns currently-executable tasks (blockers all done).
//
// goalID narrows the search to a subtree; empty means "all stateful
// categories, globally". Returns `[]Task{}` (not nil) on empty result
// so the HTTP envelope emits `[]` not `null`.
func (s *Service) Executable(ctx context.Context, goalID string, schema core.SchemaConfig) ([]core.Task, error) {
	return store.GetExecutableTasks(ctx, s.db, schema, goalID)
}

// ClaimNextTask atomically claims the highest-priority pending task for
// processing. Returns nil (not an error) when no tasks are available.
// Uses UPDATE...RETURNING to ensure exactly one worker claims each task.
func (s *Service) ClaimNextTask(ctx context.Context, goalID string, schema core.SchemaConfig) (*core.Task, error) {
	return store.ClaimNextTask(ctx, s.db, schema, goalID)
}

// List returns task entities filtered by status and/or goal subtree.
// Empty filters mean "no filter on that dimension" — both empty =
// list ALL stateful-category tasks globally. Nil→empty normalization
// promotes the downstream envelope contract.
func (s *Service) List(_ context.Context, status, goalID string, schema core.SchemaConfig) ([]core.Task, error) {
	return store.ListTasks(s.db, schema, status, goalID)
}

// Show returns one task entity plus its blocked_by + recovers_via
// edge lists.
//
// Errors: returns core.NewNotFoundError if the entity doesn't exist;
// other store errors are wrapped as-is.
func (s *Service) Show(_ context.Context, id string, schema core.SchemaConfig) (core.Task, []core.Edge, []core.Edge, error) {
	if id == "" {
		return core.Task{}, nil, nil, core.NewInvalidInputError("id required")
	}
	task, blocked, recovers, err := store.GetTaskWithRelations(s.db, schema, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return core.Task{}, nil, nil, core.NewNotFoundError(err.Error())
		}
		return core.Task{}, nil, nil, fmt.Errorf("show: %w", err)
	}
	return task, blocked, recovers, nil
}

// Dep adds or removes a dependency edge between two tasks. The HTTP
// shell does relation-type pre-validation against state.ValidRelationTypes;
// the domain treats relationType as opaque and just passes through to
// store.AddEdge / store.DeleteEdge. A missing edge on delete is
// non-fatal, a duplicate edge on add is non-fatal.
func (s *Service) Dep(_ context.Context, sourceID, targetID, relationType string, add bool) error {
	if sourceID == "" || targetID == "" {
		return core.NewInvalidInputError("dep: source_id and target_id required")
	}
	if add {
		if err := store.AddEdge(s.db, sourceID, targetID, relationType, 1.0); err != nil {
			slog.Warn("task.Dep: add edge failed", "source", sourceID, "target", targetID, "relation", relationType, "err", err)
		}
	} else {
		if err := store.DeleteEdge(s.db, sourceID, targetID, relationType); err != nil {
			slog.Warn("task.Dep: delete edge failed", "source", sourceID, "target", targetID, "relation", relationType, "err", err)
		}
	}
	return nil
}

// Rollback looks up the rollback companion task for the given task ID.
// Returns the rollback task ID (string), or an error string if no
// rollback task was found upstream (HTTP shell maps both to 500).
func (s *Service) Rollback(_ context.Context, id string, schema core.SchemaConfig) (string, error) {
	if id == "" {
		return "", fmt.Errorf("rollback: id required")
	}
	rollbackID, err := store.FindRollbackTask(s.db, schema, id)
	if err != nil {
		return "", fmt.Errorf("rollback: %w", err)
	}
	return rollbackID, nil
}

// Tree returns the rendered ASCII task tree for the given root goalID.
// Empty goalID = "render every root task tree". Returns the
// store.RenderTaskTree'd string verbatim.
func (s *Service) Tree(_ context.Context, goalID string, schema core.SchemaConfig) (string, error) {
	nodes, err := store.GetTaskTree(s.db, schema, goalID)
	if err != nil {
		return "", fmt.Errorf("tree: %w", err)
	}
	return store.RenderTaskTree(nodes, ""), nil
}

// Create is the only Service method that exercises embedder + vi.
//
// Steps, in order:
//  1. Embed content
//  2. Resolve the schema's first stateful category (the task category)
//  3. Build entity + persist via store.StoreEntityWithEmbedding
//  4. Add context_id → task edges with relation_type "related_to"
//  5. vector.AutoLinkEdges so the new task surfaces in the related_to
//     graph without a second HTTP hop
//
// Returns the new task's ID on success. id arg must be non-empty
// (the HTTP shell generates one via core.NewTaskID if the request
// omitted it). Errors at each step are wrapped with "create:" prefix
// so the HTTP shell can echo them in 500 responses.
//
// Note: this is the only Service method that stamps on the embedding
// side-effect. All other methods (Status, Executable, List, Show,
// Dep, Rollback, Tree, RecoveryPlan) are pure SQL reads.
func (s *Service) Create(ctx context.Context, id, content string, contextIDs []string, schema core.SchemaConfig) (string, error) {
	if content == "" {
		return "", fmt.Errorf("create: content required")
	}
	if id == "" {
		return "", fmt.Errorf("create: id required (HTTP shell should fill via core.NewTaskID)")
	}
	emb, err := s.embedder.Embed(ctx, content)
	if err != nil {
		slog.Warn("task.Create: embed failed, storing without embedding", "id", id, "err", err)
	}
	cat := config.FirstStatefulCategory(schema)
	if cat == "" {
		return "", core.NewInvalidInputError("no stateful category configured")
	}
	entity := core.Entity{
		ID:        id,
		Category:  cat,
		Content:   content,
		Embedding: emb,
	}
	if err := store.StoreEntityWithEmbedding(ctx, s.db, s.vi, schema, entity); err != nil {
		return "", fmt.Errorf("create: store: %w", err)
	}
	for _, cid := range contextIDs {
		if cid != "" {
			if err := store.AddEdge(s.db, id, cid, "related_to", 1.0); err != nil {
				slog.Warn("task.Create: add context edge failed", "task_id", id, "context_id", cid, "err", err)
			}
		}
	}
	vector.AutoLinkEdges(ctx, s.db, s.vi, s.embedder, id, emb) //nolint:errcheck // shadow auto-link; not a Create failure
	return id, nil
}

// RecoveryPlan returns the list of recovery entities for a task that's
// in blocked state. Nil→empty normalization so the envelope emits `[]`
// not `null` on empty plan.
func (s *Service) RecoveryPlan(_ context.Context, id string, schema core.SchemaConfig) ([]core.Task, error) {
	if id == "" {
		return nil, fmt.Errorf("recovery: id required")
	}
	return store.GenerateRecoveryPlan(s.db, schema, id)
}
