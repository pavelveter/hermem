//go:build no_local_embedding

package ai

import (
	"context"
	"fmt"
	"time"
)

// LocalEmbedder is a stub when the llama-embedding binary is not embedded.
type LocalEmbedder struct{}

// NewLocalEmbedder returns an error — local embedding is not available
// because the binary was not embedded at build time.
func NewLocalEmbedder(modelPath string, timeout time.Duration) (*LocalEmbedder, error) {
	return nil, fmt.Errorf("local embed: not available — build without 'go:embed' (bin/llama-embedding missing)")
}

// Embed always returns an error in stub mode.
func (e *LocalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("local embed: not available")
}

// Ping always returns an error in stub mode.
func (e *LocalEmbedder) Ping(ctx context.Context) error {
	return fmt.Errorf("local embed: not available")
}
