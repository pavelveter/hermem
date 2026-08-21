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

// Entity is the central domain object — a fact, opinion, experience, or observation.
//
// Deprecated: alias to the canonical pkg/domain.Entity.
type Entity = domain.Entity

// Edge is a directed relation between two entities.
//
// Deprecated: alias to the canonical pkg/domain.Edge.
type Edge = domain.Edge

// SchemaConfig defines the allowed categories, relations, and state machine.
//
// Deprecated: alias to the canonical pkg/domain.SchemaConfig.
type SchemaConfig = domain.SchemaConfig

// DefaultSchemaConfig returns a SchemaConfig with built-in defaults.
//
// Deprecated: call domain.DefaultSchemaConfig directly.
func DefaultSchemaConfig(stateful bool) SchemaConfig { return domain.DefaultSchemaConfig(stateful) }

// RankingWeight holds tunable parameters for the composite ranker.
// Zero fields are treated as "unset" — call WithDefaults to resolve a
// zero-means-unset struct into one safe to feed the ranker.
//
// Deprecated: alias to the canonical pkg/domain.RankingWeight.
type RankingWeight = domain.RankingWeight

// SearchResult pairs an entity with its cosine similarity to a query.
//
// Deprecated: alias to the canonical pkg/domain.SearchResult.
type SearchResult = domain.SearchResult

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

// Relation — a typed connection extracted from dialog.
//
// Deprecated: alias to the canonical pkg/domain.Relation.
type Relation = domain.Relation

// ExtractedEntity is one entity extracted from a dialog by an LLM.
//
// Deprecated: alias to the canonical pkg/domain.ExtractedEntity.
type ExtractedEntity = domain.ExtractedEntity

// ExtractionResult is the full output of an LLM extraction call.
//
// Deprecated: alias to the canonical pkg/domain.ExtractionResult.
type ExtractionResult = domain.ExtractionResult

// LLMExtractor runs entity+relation extraction on a dialog.
type LLMExtractor interface {
	ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error)
}

// Provenance records where an ingested entity came from.
//
// Deprecated: alias to the canonical pkg/domain.Provenance.
type Provenance = domain.Provenance

// MemoryMessage is a dialog to be processed by the ingestion pipeline.
// JSON tags normalize the surface so the pending.jsonl drain file
// (written by MemoryWorkerResilient § 4.2) is readable by Go AND
// by any external producer/language that consumes it on restart.
//
// Deprecated: alias to the canonical pkg/domain.MemoryMessage.
type MemoryMessage = domain.MemoryMessage

// Polarity represents whether evidence supports or refutes a belief.
//
// Deprecated: alias to the canonical pkg/domain.Polarity.
type Polarity = domain.Polarity

const (
	PolaritySupport = domain.PolaritySupport
	PolarityRefute  = domain.PolarityRefute
)

// TimePtr returns a pointer to t. Convenience helper for constructing
// *time.Time fields in struct literals.
//
// Deprecated: call domain.TimePtr directly.
var TimePtr = domain.TimePtr

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
	Tasks []Task `json:"tasks"`
}

type TaskListRequest struct {
	Status string `json:"status"`
	GoalID string `json:"goal_id"`
}

type TaskShowRequest struct {
	ID string `json:"id"`
}

type TaskShowResponse struct {
	Entity      Task   `json:"entity"`
	BlockedBy   []Edge `json:"blocked_by"`
	RecoversVia []Edge `json:"recovers_via"`
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
