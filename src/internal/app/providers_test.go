package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/app"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func TestVectorStoreRegistry_RegistrationAndResolution(t *testing.T) {
	registry := app.NewVectorStoreRegistry()
	store := vector.NewInMemoryVectorStore(nil, 4)

	if err := registry.Register(vector.InMemoryVectorStoreProvider, store); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register(vector.InMemoryVectorStoreProvider, store); !errors.Is(err, app.ErrProviderAlreadyRegistered) {
		t.Fatalf("duplicate Register error: want ErrProviderAlreadyRegistered, got %v", err)
	}

	resolved, err := registry.Resolve(vector.InMemoryVectorStoreProvider)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != store {
		t.Fatal("Resolve returned a different VectorStore")
	}
	if names := registry.Names(); len(names) != 1 || names[0] != vector.InMemoryVectorStoreProvider {
		t.Fatalf("Names: got %#v", names)
	}
}

func TestVectorStoreRegistry_MissingAndNilFailClosed(t *testing.T) {
	registry := app.NewVectorStoreRegistry()
	if _, err := registry.Resolve("missing"); !errors.Is(err, app.ErrProviderNotFound) {
		t.Fatalf("missing Resolve error: want ErrProviderNotFound, got %v", err)
	}

	var store spi.VectorStore
	if err := registry.Register("nil", store); !errors.Is(err, app.ErrProviderValueRequired) {
		t.Fatalf("nil Register error: want ErrProviderValueRequired, got %v", err)
	}
	if err := registry.Register("", vector.NewInMemoryVectorStore(nil, 1)); !errors.Is(err, app.ErrProviderNameRequired) {
		t.Fatalf("empty-name Register error: want ErrProviderNameRequired, got %v", err)
	}
}

func TestVectorStoreRegistryUsesCanonicalVectorContract(t *testing.T) {
	registry := app.NewVectorStoreRegistry()
	store := vector.NewInMemoryVectorStore(nil, 2)
	if err := registry.Register("memory", store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stats(context.Background(), "missing"); err != nil {
		t.Fatalf("registered store is not usable as VectorStore: %v", err)
	}
}

func TestFactoryRegistryDescriptor(t *testing.T) {
	registry := app.NewVectorStoreFactoryRegistry()
	factory := vector.InMemoryVectorStoreFactory(nil, 2)
	if err := registry.RegisterWithDescriptor("memory", factory, spi.ProviderDescriptor{
		Kind: spi.ProviderKindVectorStore, Name: "memory", Version: "v1", Dimensions: 2,
	}); err != nil {
		t.Fatal(err)
	}
	descriptor, err := registry.Descriptor("memory")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Version != "v1" || descriptor.Dimensions != 2 {
		t.Fatalf("unexpected descriptor: %#v", descriptor)
	}
}
