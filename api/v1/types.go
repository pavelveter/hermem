// Package v1 contains the versioned HTTP transport contract.
// DTOs in this package deliberately do not alias internal/core values.
package v1

import (
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/pkg/domain"
)

type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Field string `json:"field,omitempty"`
}

type StoreRequest struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

func (r StoreRequest) Validate() error {
	for name, value := range map[string]string{"id": r.ID, "category": r.Category, "content": r.Content} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func (r StoreRequest) ToDomain() domain.Entity {
	return domain.Entity{ID: r.ID, Category: r.Category, Content: r.Content, Embedding: r.Embedding}
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// SearchResult is the transport shape for one search hit.
type SearchResult struct {
	Entity     EntityDTO `json:"entity"`
	Similarity float32   `json:"similarity"`
}

func (r SearchRequest) Normalize() SearchRequest {
	if r.TopK <= 0 {
		r.TopK = 5
	}
	return r
}

func (r SearchRequest) Validate() error {
	if strings.TrimSpace(r.Query) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

type RetrieveRequest struct {
	SeedIDs  []string `json:"seed_ids"`
	MaxDepth int      `json:"max_depth,omitempty"`
}

type TemporalQueryRequest struct {
	Query    string `json:"query"`
	TopK     int    `json:"top_k,omitempty"`
	TimeFrom string `json:"time_from,omitempty"`
	TimeTo   string `json:"time_to,omitempty"`
}

func (r RetrieveRequest) Normalize() RetrieveRequest {
	if r.MaxDepth <= 0 {
		r.MaxDepth = 2
	}
	return r
}

func (r RetrieveRequest) Validate() error {
	if len(r.SeedIDs) == 0 {
		return fmt.Errorf("seed_ids is required")
	}
	return nil
}

type IngestRequest struct {
	Dialog string `json:"dialog"`
}

type EdgeRequest struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	AutoCreate   bool    `json:"auto_create,omitempty"`
	Weight       float32 `json:"weight,omitempty"`
}

func (r EdgeRequest) ToDomain() domain.Edge {
	return domain.Edge{SourceID: r.SourceID, TargetID: r.TargetID, RelationType: r.RelationType, Weight: r.Weight}
}

type TaskStatusRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskExecutableResponse struct {
	Tasks []domain.Task `json:"tasks"`
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

type TaskTreeResponse struct {
	Tree string `json:"tree"`
}

type TaskCreateRequest struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	ContextIDs []string `json:"context_ids,omitempty"`
}

type TaskCreateResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskClaimRequest struct {
	GoalID string `json:"goal_id,omitempty"`
}

type TaskClaimResponse struct {
	Task *domain.Task `json:"task,omitempty"`
}

type StoreResponse struct {
	Status string `json:"status"`
}

type QueryResponse struct {
	Context string `json:"context"`
}

type ResponseResponse struct {
	Response string `json:"response"`
}
