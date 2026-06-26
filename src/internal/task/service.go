// Package task hosts the transport-agnostic TaskService — the domain
// API for hermem's task subsystem: state transitions, listing,
// show-with-relations, dependency edges, rollback lookup, tree
// rendering, recovery-plan suggestion, and task creation with the
// embed+store+link side effect.
//
// The embedded `vector.AutoLinkEdges` call inside Create is the only
// reason this Service holds vi + embedder. All other handlers operate
// purely on the relational schema and the task DAG via store.* funcs.
// Matches memory.Service's pattern: per-call args include `schema
// core.SchemaConfig`; no Refs field; the HTTP shell loads schema per
// request and threads it here.
package task

import (
	"context"
	"database/sql"
	"fmt"
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

// NewService constructs a Service. All three deps are required; passing
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
// categories, globally". The internal SQL pulls task entities in
// pending state with no unfinished blocker via the blocked_by +
// relation-blocking paths in the schema. Returns `[]Entity{}` (not
// nil) on empty result so the HTTP envelope emits `[]` not `null`.
func (s *Service) Executable(_ context.Context, goalID string, schema core.SchemaConfig) ([]core.Entity, error) {
	tasks, err := getExecutable(s.db, schema, goalID)
	if err != nil {
		return nil, err
	}
	return core.NormalizeSlice(tasks), nil
}

// List returns task entities filtered by status and/or goal subtree.
// Empty filters mean "no filter on that dimension" — both empty =
// list ALL stateful-category tasks globally. Nil→empty normalization
// promotes the downstream envelope contract.
func (s *Service) List(_ context.Context, status, goalID string, schema core.SchemaConfig) ([]core.Entity, error) {
	tasks, err := store.ListTasks(s.db, schema, status, goalID)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	return core.NormalizeSlice(tasks), nil
}

// Show returns one task entity plus its blocked_by + recovers_via
// edge lists.
//
// Errors: returns core.NewNotFoundError if the entity doesn't exist;
// other store errors are wrapped as-is.
func (s *Service) Show(_ context.Context, id string, schema core.SchemaConfig) (core.Entity, []core.Edge, []core.Edge, error) {
	if id == "" {
		return core.Entity{}, nil, nil, core.NewInvalidInputError("id required")
	}
	entity, blocked, recovers, err := store.GetTaskWithRelations(s.db, schema, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return core.Entity{}, nil, nil, core.NewNotFoundError(err.Error())
		}
		return core.Entity{}, nil, nil, fmt.Errorf("show: %w", err)
	}
	return entity, blocked, recovers, nil
}

// Dep adds or removes a dependency edge between two tasks. The HTTP
// shell does relation-type pre-validation against state.ValidRelationTypes;
// the domain treats relationType as opaque and just passes through to
// store.AddEdge / store.DeleteEdge. A missing edge on delete is
// non-fatal, a duplicate edge on add is non-fatal.
func (s *Service) Dep(_ context.Context, sourceID, targetID, relationType string, add bool) error {
	if sourceID == "" || targetID == "" {
		return fmt.Errorf("dep: source_id and target_id required")
	}
	if add {
		_ = store.AddEdge(s.db, sourceID, targetID, relationType, 1.0) //nolint:errcheck // best-effort: edge adjacencies may be stale on store error
	} else {
		_ = store.DeleteEdge(s.db, sourceID, targetID, relationType) //nolint:errcheck // cleanup is best-effort; on failure row stays for next sweep
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
		return "", fmt.Errorf("create: embed: %w", err)
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
	if err := store.StoreEntityWithEmbedding(s.db, s.vi, schema, entity); err != nil {
		return "", fmt.Errorf("create: store: %w", err)
	}
	for _, cid := range contextIDs {
		if cid != "" {
			_ = store.AddEdge(s.db, id, cid, "related_to", 1.0) //nolint:errcheck // best-effort: edge adjacencies may be stale on store error
		}
	}
	vector.AutoLinkEdges(ctx, s.db, s.vi, s.embedder, id, emb) //nolint:errcheck // best-effort: shadow auto-link; not a Create failure
	return id, nil
}

// RecoveryPlan returns the list of recovery entities for a task that's
// in blocked state. Nil→empty normalization so the envelope emits `[]`
// not `null` on empty plan.
func (s *Service) RecoveryPlan(_ context.Context, id string, schema core.SchemaConfig) ([]core.Entity, error) {
	if id == "" {
		return nil, fmt.Errorf("recovery: id required")
	}
	plan, err := store.GenerateRecoveryPlan(s.db, schema, id)
	if err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}
	return core.NormalizeSlice(plan), nil
}

// --- internal helpers ---

// getExecutable dispatches to goal-scoped vs global SQL. tree form
// pre-PHASE-2.4 was retrieval.GetExecutableTasks + retrieval.
// getExecutableForGoal + retrieval.getExecutableGlobal; PHASE 2.4
// folds them into the task pkg and renames them to be unexported
// (private to this domain).
func getExecutable(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return []core.Entity{}, nil
	}
	if goalID != "" {
		return getExecutableForGoal(db, schema, goalID)
	}
	return getExecutableGlobal(db, schema)
}

func getExecutableForGoal(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, schema.RelationBlocking)
	args = append(args, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`WITH RECURSIVE dep_tree AS (SELECT e.id, e.category, e.content, e.status, e.updated_at FROM entities e WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0 UNION ALL SELECT e.id, e.category, e.content, e.status, e.updated_at FROM dep_tree dt JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ? JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0) SELECT dt.id, dt.category, dt.content, dt.status, dt.updated_at, COALESCE(e.priority, 0) FROM dep_tree dt JOIN entities e ON e.id = dt.id WHERE dt.status = ? AND NOT EXISTS (SELECT 1 FROM edges ed2 WHERE ed2.target_id = dt.id AND ed2.relation_type = ? AND EXISTS (SELECT 1 FROM entities e3 WHERE e3.id = ed2.source_id AND e3.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, dt.id`, catPH, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable for goal: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}

func getExecutableGlobal(db *sql.DB, schema core.SchemaConfig) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{}, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`SELECT e.id, e.category, e.content, e.status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0 AND NOT EXISTS (SELECT 1 FROM edges ed WHERE ed.target_id = e.id AND ed.relation_type = ? AND EXISTS (SELECT 1 FROM entities e2 WHERE e2.id = ed.source_id AND e2.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, e.id`, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}
