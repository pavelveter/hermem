package app_test

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/app"
	"github.com/pavelveter/hermem/src/internal/config"
)

func validProviderConfig() *config.Config {
	return &config.Config{Provider: "ollama", URL: "http://localhost:11434", VectorBackend: "in-memory"}
}

func TestValidateProviderSelectionRejectsUnknownProvider(t *testing.T) {
	cfg := validProviderConfig()
	cfg.Provider = "typo-provider"
	if err := app.ValidateProviderSelection(cfg); err == nil {
		t.Fatal("unknown provider must fail closed")
	}
}

func TestValidateProviderSelectionRejectsUnknownVectorBackend(t *testing.T) {
	cfg := validProviderConfig()
	cfg.VectorBackend = "remote-typo"
	if err := app.ValidateProviderSelection(cfg); err == nil {
		t.Fatal("unknown vector backend must fail closed")
	}
}
