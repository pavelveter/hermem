package time

import (
	"encoding/json"
	"fmt"
	stdtime "time"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newTemporalCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "temporal",
		Short: "Time-bounded retrieval (time_from / time_to in RFC3339)",
		Long: `Retrieve entities within a specific time window.

Input (JSON on stdin):
  {
    "query":     "natural language query",
    "time_from": "2024-01-01T00:00:00Z",  // RFC3339, optional
    "time_to":   "2024-12-31T23:59:59Z",  // RFC3339, optional
    "top_k":     3                         // optional, default 3
  }

Pipeline:
  1. Embed the query text.
  2. Vector search to find seed entities.
  3. Graph walk with time bounds applied — only entities created
     within the [time_from, time_to] window are included.

Time bounds filter by entity created_at timestamp. Omitting both
time_from and time_to returns all time periods (no temporal filter).

Output (JSON):
  {"world_facts":[...], "opinions":[...], ...}

Examples:
  echo '{"query":"deployment","time_from":"2024-01-01T00:00:00Z"}' | hermem time temporal
  echo '{"query":"meeting notes","time_to":"2024-06-30T23:59:59Z"}' | hermem time temporal`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Query    string `json:"query"`
				TimeFrom string `json:"time_from"`
				TimeTo   string `json:"time_to"`
				TopK     int    `json:"top_k"`
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
			emb, _ := env.Embedder.Embed(env.Ctx, req.Query)                            //nolint:errcheck // CLI: zero-vector fallback reduces to empty EmbedResult
			results, _ := vector.SearchByVector(env.Ctx, env.DB, env.VI, emb, req.TopK) //nolint:errcheck // CLI: empty results vector is rendered as `[]` upstream
			seedIDs := make([]string, 0, len(results))
			for _, r := range results {
				seedIDs = append(seedIDs, r.Entity.ID)
			}
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				TokenBudget:       env.Cfg.TokenBudget,
				QueryEmbedding:    emb,
				QueryText:         req.Query,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			if req.TimeFrom != "" {
				if t, err := stdtime.Parse(stdtime.RFC3339, req.TimeFrom); err == nil {
					opts.TimeFrom = t.UTC()
				}
			}
			if req.TimeTo != "" {
				if t, err := stdtime.Parse(stdtime.RFC3339, req.TimeTo); err == nil {
					opts.TimeTo = t.UTC()
				}
			}
			if env.Retriever == nil {
				return fmt.Errorf("retriever not available")
			}
			result, err := env.Retriever.RetrieveContext(env.Ctx, seedIDs, opts)
			if err != nil {
				return fmt.Errorf("temporal: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}
