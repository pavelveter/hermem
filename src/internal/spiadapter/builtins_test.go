package spiadapter_test

import (
	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/ai"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/spiadapter"
)

var (
	// Embedders implement spi.Embedder directly (task 4.1); the legacy
	// bridges were deleted after zero-reference verification.
	_ spi.Embedder = (*ai.NoopEmbedder)(nil)
	_ spi.Embedder = (*ai.OllamaEmbedder)(nil)
	_ spi.Embedder = (*ai.OpenAIEmbedder)(nil)
	_ spi.Embedder = (*ai.LocalEmbedder)(nil)

	// Extractors still expose the legacy LLM-ID-bearing shape; the
	// public Extract method is adapted through spiadapter until the
	// extraction DTO move completes (task 6.3).
	_ core.LLMExtractor = (*ai.OllamaLLMExtractor)(nil)
	_ core.LLMExtractor = (*ai.OpenAILLMExtractor)(nil)
	_ spi.Extractor     = spiadapter.NewExtractor((*ai.OllamaLLMExtractor)(nil))
	_ spi.Extractor     = spiadapter.NewExtractor((*ai.OpenAILLMExtractor)(nil))

	// Rerankers still implement the legacy facts-based contract; the
	// public Candidate-based adaptation happens at the application
	// boundary (app.publicRerankerFromLegacy) until the retrieval
	// capability migration lands (tasks 4.x / 6.2).
	_ spi.Reranker = (*ai.NoopReranker)(nil)
	_ spi.Reranker = (*ai.OllamaReranker)(nil)
	_ spi.Reranker = (*ai.OpenAIReranker)(nil)
)
