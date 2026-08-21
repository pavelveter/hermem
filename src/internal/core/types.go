// Package core defines the foundational domain types shared across all hermem packages.
// It has zero internal dependencies and is imported by every other package.
//
// Deprecated: the facade is being removed in the breaking release. Value
// types whose canonical home is pkg/domain are now aliases; new code must
// import pkg/domain directly.
package core

import (
	"context"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
)

// VectorIndex is the interface for vector similarity search and storage.
type VectorIndex interface {
	Search(ctx context.Context, vec []float32, limit int) ([]string, error)
	SearchBatch(ctx context.Context, vecs [][]float32, limit int) ([][]string, error)
	Store(ctx context.Context, id string, vec []float32) error
	Remove(ctx context.Context, ids []string) error
}

// Embedder converts text to a float32 embedding vector.
//
// Deprecated: alias to the canonical pkg/spi.Embedder. Health checks use
// an optional spi.Pinger assertion instead of a required Ping method.
type Embedder = spi.Embedder

// ErrorResponse carries a human message plus optional code/field.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Field string `json:"field,omitempty"`
}

// Server request/response types

type StoreRequest struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

// TemporalQueryRequest is the request body for POST /query/temporal.
type TemporalQueryRequest struct {
	Query    string `json:"query"`
	TopK     int    `json:"top_k"`
	TimeFrom string `json:"time_from,omitempty"` // RFC3339
	TimeTo   string `json:"time_to,omitempty"`   // RFC3339
}

type RetrieveRequest struct {
	SeedIDs  []string `json:"seed_ids"`
	MaxDepth int      `json:"max_depth"`
}

type IngestRequest struct {
	Dialog string `json:"dialog"`
}

type EdgeRequest struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	AutoCreate   bool    `json:"auto_create"`
	Weight       float32 `json:"weight,omitempty"`
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
