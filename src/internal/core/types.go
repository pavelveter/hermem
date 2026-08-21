// Package core defines the foundational domain types shared across all hermem packages.
// It has zero internal dependencies and is imported by every other package.
//
// Deprecated: the facade is being removed in the breaking release. Value
// types whose canonical home is pkg/domain are now aliases; new code must
// import pkg/domain directly.
package core

import (
	"context"
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
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
type Embedder interface {
	Embed(ctx context.Context, content string) ([]float32, error)
	// Ping checks whether the embedding provider is reachable.
	// Returns nil if healthy, error otherwise.
	Ping(ctx context.Context) error
}

// Retriever performs graph-walk retrieval from seed IDs.
type Retriever interface {
	// RetrieveContext runs a graph walk from seed IDs and returns ranked results.
	RetrieveContext(ctx context.Context, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error)
	// MultiHopRetrieveContext runs multiple hops of discovery, expanding seeds
	// via vector search at each hop.
	MultiHopRetrieveContext(ctx context.Context, vi VectorIndex, embedder Embedder, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error)
}

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

// ScoreBreakdown decomposes the ranking score into its constituent
// components so callers can understand why a particular node was
// retrieved. Field semantics mirror scoring.go's named contributions:
//
//	VectorScore     — cosine similarity to query (0..1)
//	RecencyScore    — exponential decay on UpdatedAt, half-life = RecencyHalfLifeHours
//	TemporalScore   — exponential decay on CreatedAt, half-life = TemporalHalfLifeHours
//	CentralityScore — log10(1 + Degree) graph centrality
//	PathScore       — cumulative edge weight from seed (path_weight)
//	DepthPenalty    — PathScore × DepthPenalty weight, subtracted from sum
//	FinalScore      — composite final ranking score (mirrors RankingScore)
//	Weights         — the ranking weights used for this score (for full explainability)
//
// ScoreBreakdown is populated when RetrieveContextOptions.Explain is true
// (or for /query/explain). Nil otherwise — the omitempty tag keeps the
// /retrieve JSON envelope byte-compatible for non-explain callers.
type ScoreBreakdown struct {
	VectorScore     float32        `json:"vector_score"`
	RecencyScore    float32        `json:"recency_score"`
	TemporalScore   float32        `json:"temporal_score"`
	CentralityScore float32        `json:"centrality_score"`
	PathScore       float32        `json:"path_score"`
	DepthPenalty    float32        `json:"depth_penalty"`
	FinalScore      float32        `json:"final_score"`
	Weights         *RankingWeight `json:"weights,omitempty"`
}

// RetrievedFact is one re-ranked item in a category bucket.
type RetrievedFact struct {
	Content        string          `json:"content"`
	ParentID       string          `json:"parent_id,omitempty"`
	RelationType   string          `json:"relation_type,omitempty"`
	Depth          int             `json:"depth"`
	VectorScore    float32         `json:"vector_score,omitempty"`
	RecencyScore   float32         `json:"recency_score,omitempty"`
	DepthPenalty   float32         `json:"depth_penalty,omitempty"`
	RankingScore   float32         `json:"ranking_score,omitempty"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
}

// Reranker reorders a list of facts based on relevance to a query.
type Reranker interface {
	Rerank(ctx context.Context, query string, facts []RetrievedFact) ([]RetrievedFact, error)
}

// GraphNode is one node returned by the graph-walk CTE.
type GraphNode struct {
	Entity         Entity          `json:"entity"`
	Relations      []Edge          `json:"relations,omitempty"`
	Depth          int             `json:"depth"`
	PathWeight     float32         `json:"path_weight,omitempty"`
	ParentID       string          `json:"parent_id"`
	RelationType   string          `json:"relation_type,omitempty"`
	RankingScore   float32         `json:"ranking_score"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
}

// RetrievalResult is the output of a RetrieveContext call.
type RetrievalResult struct {
	SeedNodes    []GraphNode     `json:"seed_nodes"`
	WorldFacts   []RetrievedFact `json:"world_facts"`
	Opinions     []RetrievedFact `json:"opinions"`
	Experiences  []RetrievedFact `json:"experiences"`
	Observations []RetrievedFact `json:"observations"`
}

// CompositeScorer computes a ranking score for a graph node.
type CompositeScorer func(node GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32

// RetrieveContextOptions controls graph-walk bounds for a single retrieval call.
type RetrieveContextOptions struct {
	TopK              int
	MaxDepth          int
	DepthCeiling      int
	MaxRetrievedNodes int
	TokenBudget       int // soft token limit; 0 = unlimited (use MaxRetrievedNodes only)
	QueryEmbedding    []float32
	CompositeScorer   CompositeScorer
	Ctx               context.Context
	Explain           bool
	RankingWeight     RankingWeight
	Reranker          Reranker
	QueryText         string
	MultiHopCount     int
	TimeFrom          time.Time
	TimeTo            time.Time
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

// ReEmbedResult is the output of ReEmbedAll.
type ReEmbedResult struct {
	TotalEntities int    `json:"total_entities"`
	ReEmbedded    int    `json:"re_embedded"`
	Skipped       int    `json:"skipped"`
	Failed        int    `json:"failed"`
	Elapsed       string `json:"elapsed"`
	OldDim        int    `json:"old_dim"`
	NewDim        int    `json:"new_dim"`
	Batches       int    `json:"batches"`
}

// VerifyReport summarises the results of VerifyGraph.
type VerifyReport struct {
	Issues []string `json:"issues"`
}

// Pass returns true if there are no issues.
func (r *VerifyReport) Pass() bool { return len(r.Issues) == 0 }

// String returns a human-readable report.
func (r *VerifyReport) String() string {
	if r.Pass() {
		return "Graph integrity verified: no issues found.\n"
	}
	s := ""
	for _, issue := range r.Issues {
		s += "  - " + issue + "\n"
	}
	return "Graph integrity issues found:\n" + s
}

// Community is the result of Louvain community detection.
type Community struct {
	ID         string   `json:"id"`
	Members    []string `json:"members"`
	Size       int      `json:"size"`
	Modularity float64  `json:"modularity"`
}

// ContradictionPair is one directed contradicts edge.
type ContradictionPair struct {
	SourceID      string `json:"source_id"`
	SourceContent string `json:"source_content"`
	TargetID      string `json:"target_id"`
	TargetContent string `json:"target_content"`
}

// ConnectedComponent is a group of mutually reachable entity IDs.
type ConnectedComponent struct {
	IDs       []string `json:"ids"`
	Size      int      `json:"size"`
	AvgDegree float64  `json:"avg_degree"`
}

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

// TreeNode represents a node in the task tree.
type TreeNode struct {
	ID       string
	Content  string
	Status   string
	Children []*TreeNode
}

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
