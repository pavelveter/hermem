// Package spi contains stable provider capability contracts for Hermem.
package spi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pavelveter/hermem/pkg/domain"
)

// ProviderKind identifies the capability implemented by a provider.
type ProviderKind string

// DefaultNamespace is the compatibility namespace used by the embedded
// single-tenant runtime. Hosted and multi-tenant composition must provide
// an application-owned namespace instead.
const DefaultNamespace = "legacy/default"

const (
	ProviderKindEmbedder    ProviderKind = "embedder"
	ProviderKindExtractor   ProviderKind = "extractor"
	ProviderKindReranker    ProviderKind = "reranker"
	ProviderKindVectorStore ProviderKind = "vector_store"
)

// Capability identifies an optional provider feature.
type Capability string

const (
	CapabilityBatchEmbedding Capability = "batch_embedding"
	CapabilityStreaming      Capability = "streaming"
)

// CapabilitySet lists the optional features a provider supports.
type CapabilitySet []Capability

// Has reports whether the set contains capability.
func (s CapabilitySet) Has(capability Capability) bool {
	for _, item := range s {
		if item == capability {
			return true
		}
	}
	return false
}

// ProviderDescriptor describes a registered provider without exposing its
// implementation or lifecycle details.
type ProviderDescriptor struct {
	Kind         ProviderKind
	Name         string
	Version      string
	Dimensions   int
	Capabilities CapabilitySet
	ConfigSpec   json.RawMessage
}

// ProviderConfig contains provider-owned configuration passed opaquely by
// the application composition layer.
type ProviderConfig struct {
	Name string
	Raw  json.RawMessage
}

// Embedder converts text to a vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BatchEmbedder is an optional embedding capability.
type BatchEmbedder interface {
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// Pinger is an optional health-check capability. Embedders that can reach a
// remote endpoint expose Ping; the health probe uses it when available.
type Pinger interface {
	Ping(ctx context.Context) error
}

// DescribedProvider exposes provider metadata when available.
type DescribedProvider interface {
	Descriptor() ProviderDescriptor
}

// Extractor extracts provider-neutral entity drafts from a dialog.
type Extractor interface {
	Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error)
}

// ExtractRequest is the provider-neutral extractor input.
type ExtractRequest struct {
	Dialog        string
	PromptVersion string
	Schema        SchemaDescriptor
}

// SchemaDescriptor identifies the extraction schema without coupling it to a
// configuration or persistence implementation.
type SchemaDescriptor struct {
	Name       string
	Version    string
	Categories []string
}

// ExtractResponse is the provider-neutral extractor output.
type ExtractResponse struct {
	Entities []domain.EntityDraft
	Usage    Usage
}

// Usage is normalized provider usage accounting.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Candidate is the domain-neutral input/output item for reranking.
type Candidate struct {
	ID       string
	Text     string
	Score    float32
	Metadata map[string]string
}

// Reranker reorders candidates for a query.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []Candidate) ([]Candidate, error)
}

// NumericRange is an inclusive numeric filter range. A nil bound is open.
type NumericRange struct {
	Min *float64
	Max *float64
}

// Filter is the deliberately small backend-neutral vector filter language.
type Filter struct {
	Equals map[string]string
	Ranges map[string]NumericRange
}

// SearchRequest describes a namespace-scoped vector search.
type SearchRequest struct {
	Namespace string
	Vector    []float32
	Limit     int
	Filter    Filter
}

// Hit is a scored vector search result. Higher scores are more similar.
type Hit struct {
	ID    string
	Score float32
}

// VectorRecord is an idempotent namespace-scoped vector upsert.
type VectorRecord struct {
	Namespace string
	ID        string
	Vector    []float32
	Metadata  map[string]string
}

// DeleteRequest describes vector records to delete.
type DeleteRequest struct {
	Namespace string
	IDs       []string
}

// VectorStats describes one vector namespace.
type VectorStats struct {
	Namespace    string
	Count        int
	Dimensions   int
	ModelProfile string
}

// VectorStore is the canonical scored, filtered, namespaced vector contract.
type VectorStore interface {
	Search(ctx context.Context, req SearchRequest) ([]Hit, error)
	Upsert(ctx context.Context, records []VectorRecord) error
	Delete(ctx context.Context, req DeleteRequest) error
	Stats(ctx context.Context, namespace string) (VectorStats, error)
}

// Typed factories keep provider resolution capability-specific.
type EmbedderFactory func(ctx context.Context, cfg ProviderConfig) (Embedder, error)
type ExtractorFactory func(ctx context.Context, cfg ProviderConfig) (Extractor, error)
type RerankerFactory func(ctx context.Context, cfg ProviderConfig) (Reranker, error)
type VectorStoreFactory func(ctx context.Context, cfg ProviderConfig) (VectorStore, error)

// ErrUnsupported marks an unavailable optional capability.
var ErrUnsupported = errors.New("spi: unsupported")

// UnsupportedCapabilityError identifies an unavailable capability.
type UnsupportedCapabilityError struct {
	Capability Capability
}

func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("spi: unsupported capability %q", e.Capability)
}

func (e *UnsupportedCapabilityError) Unwrap() error { return ErrUnsupported }

// DimensionError identifies a vector dimension mismatch.
type DimensionError struct {
	Namespace string
	Want      int
	Got       int
}

func (e *DimensionError) Error() string {
	return fmt.Sprintf("spi: namespace %q requires dimension %d, got %d", e.Namespace, e.Want, e.Got)
}

// CapacityError identifies an explicit vector capacity failure.
type CapacityError struct {
	Namespace string
	Reason    string
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("spi: vector capacity for namespace %q unavailable: %s", e.Namespace, e.Reason)
}

// PartialBatchError identifies records that failed during an otherwise
// partially applied batch operation.
type PartialBatchError struct {
	FailedIDs []string
	Cause     error
}

func (e *PartialBatchError) Error() string {
	return fmt.Sprintf("spi: vector batch partially failed for %d records", len(e.FailedIDs))
}

func (e *PartialBatchError) Unwrap() error { return e.Cause }
