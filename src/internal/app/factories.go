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
)

// FactoryRegistry stores typed provider factories for one capability. It is
// deliberately separate from ProviderRegistry: factories construct resources,
// while ProviderRegistry holds the application-owned resolved instances.
type FactoryRegistry[T any] struct {
	kind        spi.ProviderKind
	mu          sync.RWMutex
	factories   map[string]func(context.Context, spi.ProviderConfig) (T, error)
	descriptors map[string]spi.ProviderDescriptor
}

func NewFactoryRegistry[T any](kind spi.ProviderKind) *FactoryRegistry[T] {
	return &FactoryRegistry[T]{
		kind:        kind,
		factories:   make(map[string]func(context.Context, spi.ProviderConfig) (T, error)),
		descriptors: make(map[string]spi.ProviderDescriptor),
	}
}

func (r *FactoryRegistry[T]) Register(name string, factory func(context.Context, spi.ProviderConfig) (T, error)) error {
	return r.RegisterWithDescriptor(name, factory, spi.ProviderDescriptor{Kind: r.kind, Name: name})
}

// RegisterWithDescriptor registers a factory and its stable capability
// metadata in one application-owned composition operation.
func (r *FactoryRegistry[T]) RegisterWithDescriptor(name string, factory func(context.Context, spi.ProviderConfig) (T, error), descriptor spi.ProviderDescriptor) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &ProviderError{Kind: r.kind, Err: ErrProviderNameRequired}
	}
	if isNilFactory(factory) {
		return &ProviderError{Kind: r.kind, Name: name, Err: errors.New("provider factory required")}
	}
	if descriptor.Kind == "" {
		descriptor.Kind = r.kind
	}
	if descriptor.Name == "" {
		descriptor.Name = name
	}
	if descriptor.Kind != r.kind || descriptor.Name != name {
		return &ProviderError{Kind: r.kind, Name: name, Err: fmt.Errorf("provider descriptor does not match registry")}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderAlreadyRegistered}
	}
	r.factories[name] = factory
	r.descriptors[name] = descriptor
	return nil
}

// Descriptor returns metadata for a registered factory.
func (r *FactoryRegistry[T]) Descriptor(name string) (spi.ProviderDescriptor, error) {
	name = strings.TrimSpace(name)
	r.mu.RLock()
	descriptor, ok := r.descriptors[name]
	r.mu.RUnlock()
	if !ok {
		return spi.ProviderDescriptor{}, &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderNotFound}
	}
	return descriptor, nil
}

func (r *FactoryRegistry[T]) Resolve(ctx context.Context, name string, cfg spi.ProviderConfig) (T, error) {
	name = strings.TrimSpace(name)
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		var zero T
		return zero, &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderNotFound}
	}
	provider, err := factory(ctx, cfg)
	if err != nil {
		return provider, &ProviderError{Kind: r.kind, Name: name, Err: err}
	}
	if isNilProvider(provider) {
		var zero T
		return zero, &ProviderError{Kind: r.kind, Name: name, Err: ErrProviderValueRequired}
	}
	return provider, nil
}

func (r *FactoryRegistry[T]) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

func isNilFactory[T any](factory func(context.Context, spi.ProviderConfig) (T, error)) bool {
	if factory == nil {
		return true
	}
	return reflect.ValueOf(factory).IsNil()
}

type EmbedderFactoryRegistry = FactoryRegistry[spi.Embedder]
type ExtractorFactoryRegistry = FactoryRegistry[spi.Extractor]
type RerankerFactoryRegistry = FactoryRegistry[spi.Reranker]
type VectorStoreFactoryRegistry = FactoryRegistry[spi.VectorStore]

func NewEmbedderFactoryRegistry() *EmbedderFactoryRegistry {
	return NewFactoryRegistry[spi.Embedder](spi.ProviderKindEmbedder)
}
func NewExtractorFactoryRegistry() *ExtractorFactoryRegistry {
	return NewFactoryRegistry[spi.Extractor](spi.ProviderKindExtractor)
}
func NewRerankerFactoryRegistry() *RerankerFactoryRegistry {
	return NewFactoryRegistry[spi.Reranker](spi.ProviderKindReranker)
}
func NewVectorStoreFactoryRegistry() *VectorStoreFactoryRegistry {
	return NewFactoryRegistry[spi.VectorStore](spi.ProviderKindVectorStore)
}

// RegisterBuiltInFactory is a small diagnostic helper used by composition
// tests to ensure a built-in is registered exactly once.
func RegisterBuiltInFactory[T any](registry *FactoryRegistry[T], name string, factory func(context.Context, spi.ProviderConfig) (T, error)) error {
	if registry == nil {
		return fmt.Errorf("nil provider factory registry")
	}
	return registry.RegisterWithDescriptor(name, factory, spi.ProviderDescriptor{Kind: registry.kind, Name: name})
}
