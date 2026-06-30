// Package orchestrator manages task execution — agent loop and execution
// planning. Transport-agnostic domain service; no HTTP or CLI shell.
// Programmatic invocation only (tests, embedded callers).
package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

// Service is the transport-agnostic task-orchestrator domain service.
// One dep — db — matches the graph.Service precedent (PHASE 3.1).
// Schema is passed per call so SIGHUP reloads apply without reconstructing
// the service.
type Service struct {
	db *sql.DB
}

// New constructs a Service. db is required.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// AgentLoop executes tasks in a goal's dependency tree in topological order.
// Uses exponential backoff (50ms → 1s) when no tasks are ready, instead of
// a fixed 500ms busy-wait. The loop breaks immediately once all tasks are
// done or the context is cancelled.
func (s *Service) AgentLoop(ctx context.Context, schema core.SchemaConfig, goalID string, execFunc func(context.Context, core.Entity) error) error {
	const (
		initBackoff = 50 * time.Millisecond
		maxBackoff  = 1 * time.Second
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		tasks, err := s.resolveExecutableTasks(ctx, schema, goalID)
		if err != nil {
			return fmt.Errorf("agent loop: get executable: %w", err)
		}
		if len(tasks) == 0 {
			break
		}
		for _, task := range tasks {
			if err := s.executeTask(ctx, schema, task, execFunc); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(initBackoff):
		}
	}
	return nil
}

// executeTask runs execFunc for a single task, recovers panics, and
// transitions the task to failed or unblocking status.
func (s *Service) executeTask(ctx context.Context, schema core.SchemaConfig, task core.Task, execFunc func(context.Context, core.Entity) error) error {
	execFailed := false
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("agent loop: exec panic", "task_id", task.ID, "recover", rec)
				execFailed = true
			}
		}()
		if err := execFunc(ctx, core.ComposeFromTask(task)); err != nil {
			slog.Error("agent loop: exec", "task_id", task.ID, "error", err)
			execFailed = true
		}
	}()
	svc := taskdomain.New(s.db, nil, nil)
	status := schema.StateUnblocking
	if execFailed {
		status = "failed"
	}
	if err := svc.Status(ctx, task.ID, status, schema); err != nil {
		return fmt.Errorf("agent loop: set status %s: %w", task.ID, err)
	}
	return nil
}

// ExecutionPlan returns executable tasks for a goal in topological order.
func (s *Service) ExecutionPlan(ctx context.Context, schema core.SchemaConfig, goalID string) ([]core.Task, error) {
	return s.resolveExecutableTasks(ctx, schema, goalID)
}

// resolveExecutableTasks queries the task domain for tasks that are
// unblocked and ready to execute. PHASE 2.4 redirection: previously
// called retrieval.GetExecutableTasks (now in taskdomain.Service.Executable).
func (s *Service) resolveExecutableTasks(ctx context.Context, schema core.SchemaConfig, goalID string) ([]core.Task, error) {
	// taskdomain.NewService requires an embedder + vi; AgentLoop +
	// ExecutionPlan don't read either, so we pass nil. Service.Executable
	// never touches embedder or vi internally.
	svc := taskdomain.New(s.db, nil, nil)
	return svc.Executable(ctx, goalID, schema)
}
