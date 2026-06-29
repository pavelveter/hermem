package hermem

import "context"

// TaskClient handles task lifecycle operations.
type TaskClient struct {
	c *Client
}

// Create creates a new task entity.
func (t *TaskClient) Create(ctx context.Context, req *TaskCreateRequest) (*TaskCreateResponse, error) {
	var result TaskCreateResponse
	err := t.c.do(ctx, "POST", "/task/create", req, &result)
	return &result, err
}

// Status updates a task's lifecycle state.
func (t *TaskClient) Status(ctx context.Context, req *TaskStatusRequest) error {
	return t.c.doNoContent(ctx, "POST", "/task/status", req)
}

// List returns tasks filtered by status and/or goal_id.
func (t *TaskClient) List(ctx context.Context, req *TaskListRequest) (*TaskExecutableResponse, error) {
	var result TaskExecutableResponse
	err := t.c.do(ctx, "POST", "/task/list", req, &result)
	return &result, err
}

// Show returns a task with its blocked_by and recovers_via edges.
func (t *TaskClient) Show(ctx context.Context, req *TaskShowRequest) (*TaskShowResponse, error) {
	var result TaskShowResponse
	err := t.c.do(ctx, "POST", "/task/show", req, &result)
	return &result, err
}

// Dep adds or removes a dependency edge between tasks.
func (t *TaskClient) Dep(ctx context.Context, req *TaskDepRequest) error {
	return t.c.doNoContent(ctx, "POST", "/task/dep", req)
}

// Tree returns an ASCII rendering of the task dependency tree.
func (t *TaskClient) Tree(ctx context.Context, req *TaskTreeRequest) (*TaskTreeResponse, error) {
	var result TaskTreeResponse
	err := t.c.do(ctx, "POST", "/task/tree", req, &result)
	return &result, err
}

// Rollback finds the task linked via recovers_via from a failed task.
func (t *TaskClient) Rollback(ctx context.Context, req *TaskRollbackRequest) (*TaskRollbackResponse, error) {
	var result TaskRollbackResponse
	err := t.c.do(ctx, "POST", "/task/rollback", req, &result)
	return &result, err
}

// Executable returns pending tasks whose dependencies are all completed.
func (t *TaskClient) Executable(ctx context.Context, req *TaskListRequest) (*TaskExecutableResponse, error) {
	var result TaskExecutableResponse
	err := t.c.do(ctx, "POST", "/task/executable", req, &result)
	return &result, err
}

// Next is an alias for Executable.
func (t *TaskClient) Next(ctx context.Context, req *TaskListRequest) (*TaskExecutableResponse, error) {
	return t.Executable(ctx, req)
}
