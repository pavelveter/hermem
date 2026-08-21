package core

import "github.com/pavelveter/hermem/pkg/domain"

// Task captures the stateful-lifecycle meta-block attached to an
// Entity when its category is stateful.
//
// Deprecated: alias to the canonical pkg/domain.Task.
type Task = domain.Task

// TaskClaimRequest is the request body for POST /task/claim-next.
type TaskClaimRequest struct {
	GoalID string `json:"goal_id,omitempty"`
}

// TaskClaimResponse is the response body for POST /task/claim-next.
type TaskClaimResponse struct {
	Task *Task `json:"task,omitempty"`
}
