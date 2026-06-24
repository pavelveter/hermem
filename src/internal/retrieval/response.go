package retrieval

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GenerateResponse is the one-shot "answer" pipeline: embed the user query,
// pull the top-K similar entities as graph seeds, walk the graph, and
// re-rank — then format the result as a human-readable Markdown block.
//
// This is the function that backs both the `hermem response` CLI subcommand
// and the `/response` HTTP endpoint, preserving parity between the two
// entry points.
//
// userQuery is the raw text query; opts carries depth/ranking settings,
// opts.QueryEmbedding/QueryText/Ctx are populated here so the inner walk
// re-uses the same query vector for re-ranking consistency.
func GenerateResponse(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, opts core.RetrieveContextOptions, userQuery string) (string, error) {
	if userQuery == "" {
		return "", fmt.Errorf("userQuery is required")
	}
	queryEmbedding, err := embedder.Embed(ctx, userQuery)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}
	seedIDs, err := vi.Search(ctx, queryEmbedding, 3)
	if err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}

	// Safe mutation: opts is the value-type copy owned by GenerateResponse,
	// not the caller's struct.
	opts.QueryEmbedding = queryEmbedding
	opts.QueryText = userQuery
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}

	result, err := RetrieveContext(db, seedIDs, opts)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve context: %w", err)
	}
	return FormatContextMarkdown(result), nil
}
