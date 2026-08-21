package app

import (
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/config"
)

// ValidateProviderSelection fails closed for unknown provider names. The
// previous AI factory silently selected Ollama for typos, which made startup
// diagnostics misleading and bypassed the typed registry contract.
func ValidateProviderSelection(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("provider config is nil")
	}
	if err := config.ValidateProviderConfig(spi.ProviderKindEmbedder, cfg.ProviderConfig(spi.ProviderKindEmbedder)); err != nil {
		return err
	}
	provider := strings.ToLower(cfg.Provider)
	if provider == "" {
		provider = "ollama"
	}
	if !oneOf(provider, "ollama", "openai", "local") {
		return fmt.Errorf("embedder provider %q is unsupported", cfg.Provider)
	}
	extractor := cfg.ExtractProvider
	if extractor == "" {
		extractor = provider
	}
	if !oneOf(strings.ToLower(extractor), "ollama", "openai") {
		return fmt.Errorf("extractor provider %q is unsupported", extractor)
	}
	if cfg.RerankerProvider != "" && !oneOf(strings.ToLower(cfg.RerankerProvider), "none", "ollama", "openai") {
		return fmt.Errorf("reranker provider %q is unsupported", cfg.RerankerProvider)
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.VectorBackend))
	if backend == "" {
		backend = "in-memory"
	}
	if backend != "in-memory" {
		return fmt.Errorf("vector store provider %q is unsupported in this release", backend)
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
