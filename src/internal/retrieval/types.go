// Package retrieval hosts the read-side domain logic — graph-walk
// retrieval from seed IDs, vector search, query → markdown formatting,
// response generation, explanation, and provenance lookup.
//
// This file defines the retrieval-owned value contract: graph and result
// envelopes passed between the read-side stages (expand → rank →
// assemble → render) and the transport shells. The types live here —
// not in pkg/domain — because they are retrieval-internal algorithm
// artifacts (final ranker scores, explanation breakdowns, re-rankable
// bucket facts) and depend on the scored ranker/embedder contracts that
// pkg/domain intentionally doesn't expose.
package retrieval

import (
	"context"
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
)

// SearchResult pairs an entity with its cosine similarity to a query.
// Re-exported as an alias so retrieval package consumers can use the
// unprefixed name without re-importing pkg/domain. The canonical
// definition lives in pkg/domain/search_result.go.
type SearchResult = domain.SearchResult

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
	VectorScore     float32               `json:"vector_score"`
	RecencyScore    float32               `json:"recency_score"`
	TemporalScore   float32               `json:"temporal_score"`
	CentralityScore float32               `json:"centrality_score"`
	PathScore       float32               `json:"path_score"`
	DepthPenalty    float32               `json:"depth_penalty"`
	FinalScore      float32               `json:"final_score"`
	Weights         *domain.RankingWeight `json:"weights,omitempty"`
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

// GraphNode is one node returned by the graph-walk CTE.
type GraphNode struct {
	Entity         domain.Entity   `json:"entity"`
	Relations      []domain.Edge   `json:"relations,omitempty"`
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

// CompositeScorer computes a ranking score for a graph node. The function
// signature is owned by retrieval and consumed by the rank stage (see
// scoring.go's defaultCompositeScorer and the PipelineStage interface in
// pipeline.go).
type CompositeScorer func(node GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32

// Reranker is the canonical ordering contract used by the retrieval
// pipeline. It is a type alias for spi.Reranker (the canonical public
// contract); callers operate on spi.Candidate shapes directly. The
// walk pipeline translates RetrievedFact → spi.Candidate at the
// applyReranker boundary so internal data structures do not leak into
// the public contract.
type Reranker = spi.Reranker

// Retriever is the read-side application contract owned by the retrieval
// package. Implementations consume the local RetrieveContext /
// RetrieveResult / RetrievedFact shapes; the capability parameters are the
// canonical pkg/spi contracts. The full ADR-037 pipeline-SPI/hybrid-channel
// redesign remains a separate project.
type Retriever interface {
	RetrieveContext(ctx context.Context, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error)
	MultiHopRetrieveContext(ctx context.Context, vi spi.VectorStore, embedder spi.Embedder, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error)
}

// RankingWeight is a cross-package config value (config/ini parses the
// values into it, retrieval/walk applies them). Aliased to
// pkg/domain.RankingWeight so config does not need to import retrieval.
type RankingWeight = domain.RankingWeight

// RetrieveContextOptions controls graph-walk bounds for a single retrieval call.
//
// Reranker is the canonical spi.Reranker contract. The walk pipeline
// performs the []RetrievedFact ↔ []spi.Candidate translation internally
// at applyReranker time; callers can pass any spi.Reranker (LLM-based,
// cross-encoder, no-op) directly without bridging.
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
	RankingWeight     domain.RankingWeight
	Reranker          Reranker
	QueryText         string
	MultiHopCount     int
	TimeFrom          time.Time
	TimeTo            time.Time
}
