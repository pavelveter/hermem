package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newQueryCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Embed → vector search → graph walk → Markdown context blob",
		Long: `Full retrieval pipeline: embed query, vector search, graph walk, return Markdown.

Input (JSON on stdin):
  {
    "query": "natural language question",
    "top_k": 5                           // optional, seeds per search
  }

Pipeline steps:
  1. Embed the query text using the configured embedder.
  2. Vector search to find top-K seed entities.
  3. Graph walk from seeds (depth 2 by default) to discover context.
  4. Score and rank all discovered entities.
  5. Render the result as a single Markdown context blob.

Output (JSON):
  {"context": "# retrieved context\n\n..."}

The Markdown output is designed to be injected directly into an LLM
prompt as grounding context. Use "hermem memory response" to have the
LLM generate an answer using this context.

Examples:
  echo '{"query":"How does garbage collection work?"}' | hermem memory query
  echo '{"query":"deployment checklist","top_k":3}' | hermem memory query | jq -r '.context'`,
		Args: cobra.NoArgs,
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
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				TokenBudget:       env.Cfg.TokenBudget,
				QueryText:         req.Query,
				Ctx:               env.Ctx,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			markdown, err := svc.Query(env.Ctx, req.Query, req.TopK, opts)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
				"context": markdown,
			})
		},
	}
}
