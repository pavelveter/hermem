package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/extraction"
	"github.com/pavelveter/hermem/src/internal/spiadapter"
)

// Provider registry errors are stable sentinels so callers can branch with
// errors.Is without depending on provider-specific error text.
var (
	ErrProviderAlreadyRegistered = errors.New("provider already registered")
	ErrProviderNotFound          = errors.New("provider not found")
	ErrProviderNameRequired      = errors.New("provider name required")
	ErrProviderValueRequired     = errors.New("provider value required")
)

// ProviderError adds capability and provider identity to a registry failure.
type ProviderError struct {
	Kind spi.ProviderKind
	Name string
	Err  error
}

func (e *ProviderError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("%s provider: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("%s provider %q: %v", e.Kind, e.Name, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// ProviderRegistry is a capability-typed, application-owned registry.
// Registration stores already-constructed providers for this phase; task 5.2
// will add explicit typed factory registration without changing resolution or
// error semantics. The registry has no global state and no init side effects.
type ProviderRegistry[T any] struct {
	kind      spi.ProviderKind
	mu        sync.RWMutex
	providers map[string]T
}

// NewProviderRegistry creates an isolated registry for one capability.
func NewProviderRegistry[T any](kind spi.ProviderKind) *ProviderRegistry[T] {
	return &ProviderRegistry[T]{
		kind:      kind,
		providers: make(map[string]T),
	}
}

// Register adds provider under name. Names are exact registry keys; config
// composition is responsible for normalizing user-facing names before this
// boundary. Duplicate registrations fail rather than replacing a provider.
func (r *ProviderRegistry[T]) Register(name string, provider T) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &ProviderError{Kind: r.kind, Err: ErrProviderNameRequired}
	}
	if isNilProvider(provider) {
		return &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderValueRequired}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderAlreadyRegistered}
	}
	r.providers[name] = provider
	return nil
}

// Resolve returns the registered provider or an explicit missing-provider
// error. It never falls back to an arbitrary provider.
func (r *ProviderRegistry[T]) Resolve(name string) (T, error) {
	name = strings.TrimSpace(name)
	r.mu.RLock()
	provider, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		var zero T
		return zero, &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderNotFound}
	}
	return provider, nil
}

// Names returns registered provider names in deterministic order.
func (r *ProviderRegistry[T]) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Specialized aliases keep capability ownership visible at call sites while
// sharing the tested typed implementation.
type EmbedderRegistry = ProviderRegistry[spi.Embedder]
type ExtractorRegistry = ProviderRegistry[spi.Extractor]
type RerankerRegistry = ProviderRegistry[spi.Reranker]
type VectorStoreRegistry = ProviderRegistry[spi.VectorStore]

func NewEmbedderRegistry() *EmbedderRegistry {
	return NewProviderRegistry[spi.Embedder](spi.ProviderKindEmbedder)
}

func NewExtractorRegistry() *ExtractorRegistry {
	return NewProviderRegistry[spi.Extractor](spi.ProviderKindExtractor)
}

func NewRerankerRegistry() *RerankerRegistry {
	return NewProviderRegistry[spi.Reranker](spi.ProviderKindReranker)
}

func NewVectorStoreRegistry() *VectorStoreRegistry {
	return NewProviderRegistry[spi.VectorStore](spi.ProviderKindVectorStore)
}

// NewConfiguredEmbedder resolves the configured built-in embedder through an
// application-owned typed factory registry. The public embedder is returned
// directly; health checks use spi.Pinger via type assertion at the one call
// site that needs it (cli/serve.go), so no legacy facade is produced here.
func NewConfiguredEmbedder(cfg *config.Config) (spi.Embedder, error) {
	registry := NewEmbedderFactoryRegistry()
	for _, name := range []string{"ollama", "openai", "local"} {
		if err := registry.RegisterWithDescriptor(name, func(context.Context, spi.ProviderConfig) (spi.Embedder, error) {
			return cfg.NewEmbedder(), nil
		}, spi.ProviderDescriptor{Kind: spi.ProviderKindEmbedder, Name: name, Version: "v1", Dimensions: cfg.VectorDim}); err != nil {
			return nil, err
		}
	}
	name := cfg.Provider
	if name == "" {
		name = "ollama"
	}
	public, err := registry.Resolve(context.Background(), name, cfg.ProviderConfig(spi.ProviderKindEmbedder))
	if err != nil {
		return nil, err
	}
	return public, nil
}

// NewConfiguredExtractor resolves extraction through the typed registry.
func NewConfiguredExtractor(cfg *config.Config) (extraction.LLMExtractor, error) {
	registry := NewExtractorFactoryRegistry()
	for _, name := range []string{"ollama", "openai"} {
		if err := registry.RegisterWithDescriptor(name, func(context.Context, spi.ProviderConfig) (spi.Extractor, error) {
			return spiadapter.NewExtractor(cfg.NewExtractor()), nil
		}, spi.ProviderDescriptor{Kind: spi.ProviderKindExtractor, Name: name, Version: "v1"}); err != nil {
			return nil, err
		}
	}
	name := cfg.ExtractProvider
	if name == "" {
		name = cfg.Provider
	}
	if name == "" {
		name = "ollama"
	}
	public, err := registry.Resolve(context.Background(), name, cfg.ProviderConfig(spi.ProviderKindExtractor))
	if err != nil {
		return nil, err
	}
	return legacyExtractorFromPublic(public), nil
}

// NewConfiguredReranker resolves the optional reranker through the typed
// registry. Empty configuration selects the explicit no-op provider.
// The configured provider implementations are still legacy spi.Reranker
// values (task 4.x capability migration); the public boundary adapts them
// to spi.Reranker with an inline RetrievedFact ↔ Candidate translation so
// internal shapes never leak into the registry contract.
func NewConfiguredReranker(cfg *config.Config) (spi.Reranker, error) {
	registry := NewRerankerFactoryRegistry()
	for _, name := range []string{"none", "ollama", "openai"} {
		if err := registry.RegisterWithDescriptor(name, func(context.Context, spi.ProviderConfig) (spi.Reranker, error) {
			return cfg.NewReranker(), nil
		}, spi.ProviderDescriptor{Kind: spi.ProviderKindReranker, Name: name, Version: "v1"}); err != nil {
			return nil, err
		}
	}
	name := cfg.RerankerProvider
	if name == "" {
		name = "none"
	}
	public, err := registry.Resolve(context.Background(), name, cfg.ProviderConfig(spi.ProviderKindReranker))
	if err != nil {
		return nil, err
	}
	return public, nil
}

// The current service layer still consumes the legacy extractor shape for
// its LLM-ID-bearing ExtractionResult pipeline; the adapter remains at the
// boundary.
func legacyExtractorFromPublic(public spi.Extractor) extraction.LLMExtractor {
	return &publicExtractorAdapter{public: public}
}

type publicExtractorAdapter struct{ public spi.Extractor }

func (a *publicExtractorAdapter) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	response, err := a.public.Extract(ctx, spi.ExtractRequest{Dialog: dialog})
	if err != nil {
		return nil, err
	}
	result := &core.ExtractionResult{}
	for _, entity := range response.Entities {
		result.Entities = append(result.Entities, core.ExtractedEntity{Category: entity.Category, Content: entity.Content})
	}
	return result, nil
}

func isNilProvider[T any](provider T) bool {
	value := any(provider)
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
