package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newQueryCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Embed → vector search → graph walk → Markdown context blob",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Query string `json:"query"`
				TopK  int    `json:"top_k,omitempty"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Query == "" {
				return fmt.Errorf("query required")
			}
			if req.TopK <= 0 {
				req.TopK = 3
			}
			emb, _ := env.Embedder.Embed(env.Ctx, req.Query)
			results, _ := vector.SearchByVector(env.DB, env.VI, emb, req.TopK)
			seedIDs := make([]string, 0, len(results))
			for _, r := range results {
				seedIDs = append(seedIDs, r.Entity.ID)
			}
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				QueryEmbedding:    emb,
				QueryText:         req.Query,
				Ctx:               env.Ctx,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			ctxResult, err := retrieval.RetrieveContext(env.DB, seedIDs, opts)
			if err != nil {
				return fmt.Errorf("retrieve: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
				"context": retrieval.FormatContextMarkdown(ctxResult),
			})
		},
	}
}
