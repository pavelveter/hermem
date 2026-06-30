package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/app"
	"github.com/pavelveter/hermem/src/internal/config"
)

// TestNew_NoNilFields asserts the core contract: every exported field
// on Application is non-nil after New() returns successfully.
func TestNew_NoNilFields(t *testing.T) {
	cfg := &config.Config{
		DBPath:          t.TempDir() + "/test.db",
		VectorDim:       128,
		AutoMigrate:     true,
		VectorBackend:   "in-memory",
		Provider:        "ollama",
		URL:             "http://localhost:11434",
		Model:           "nomic-embed-text",
		ExtractProvider: "ollama",
		ExtractURL:      "http://localhost:11434",
		ExtractModel:    "qwen2.5-coder:7b",
		EmbedderTimeout: 30 * time.Second,
		ExtractTimeout:  60 * time.Second,
	}

	build := app.BuildInfo{
		Version:   "test",
		BuildDate: "2025-01-01",
		GitCommit: "abc123",
	}

	a, err := app.New(context.Background(), cfg, build)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Every field must be non-nil.
	if a.DB == nil {
		t.Error("DB is nil")
	}
	if a.VI == nil {
		t.Error("VI is nil")
	}
	if a.Worker == nil {
		t.Error("Worker is nil")
	}
	if a.Embedder == nil {
		t.Error("Embedder is nil")
	}
	if a.Extractor == nil {
		t.Error("Extractor is nil")
	}
	if a.Reranker == nil {
		t.Error("Reranker is nil")
	}
	if a.Retriever == nil {
		t.Error("Retriever is nil")
	}
	if a.Metrics == nil {
		t.Error("Metrics is nil")
	}
	if a.Tracer == nil {
		t.Error("Tracer is nil")
	}
	if a.Cfg == nil {
		t.Error("Cfg is nil")
	}
}

// TestStop_IsIdempotent verifies that calling Stop multiple times
// does not panic or return an error on the second call.
func TestStop_IsIdempotent(t *testing.T) {
	cfg := &config.Config{
		DBPath:          t.TempDir() + "/test.db",
		VectorDim:       128,
		AutoMigrate:     true,
		VectorBackend:   "in-memory",
		Provider:        "ollama",
		URL:             "http://localhost:11434",
		Model:           "nomic-embed-text",
		ExtractProvider: "ollama",
		ExtractURL:      "http://localhost:11434",
		ExtractModel:    "qwen2.5-coder:7b",
		EmbedderTimeout: 30 * time.Second,
		ExtractTimeout:  60 * time.Second,
	}

	a, err := app.New(context.Background(), cfg, app.BuildInfo{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop #1: %v", err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop #2 (idempotent): %v", err)
	}
}
