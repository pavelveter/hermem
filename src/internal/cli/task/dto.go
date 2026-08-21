package task

import "github.com/pavelveter/hermem/pkg/domain"

// Command-local request/response payloads for the task CLI commands.
// They mirror the v1 wire shapes so stdin JSON and stdout bytes are
// unchanged, but deliberately do not import the HTTP DTO package —
// the CLI owns its transport vocabulary (task 4.6).

type TaskStatusRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskListRequest struct {
	Status string `json:"status"`
	GoalID string `json:"goal_id"`
}

type TaskShowRequest struct {
	ID string `json:"id"`
}

type TaskShowResponse struct {
	Entity      domain.Task   `json:"entity"`
	BlockedBy   []domain.Edge `json:"blocked_by"`
	RecoversVia []domain.Edge `json:"recovers_via"`
}

type TaskDepRequest struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	Add          bool   `json:"add"`
}

type TaskRollbackRequest struct {
	ID string `json:"id"`
}

type TaskRollbackResponse struct {
	RollbackTaskID string `json:"rollback_task_id"`
}

type TaskTreeRequest struct {
	GoalID string `json:"goal_id"`
}

type TaskExecutableResponse struct {
	Tasks []domain.Task `json:"tasks"`
}

type TaskCreateRequest struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	ContextIDs []string `json:"context_ids,omitempty"`
}
