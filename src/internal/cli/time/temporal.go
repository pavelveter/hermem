package time

import (
	"encoding/json"
	"fmt"
	stdtime "time"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newTemporalCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "temporal",
		Short: "Time-bounded retrieval (time_from / time_to in RFC3339)",
		Args:  cobra.NoArgs,
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
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			if req.TimeFrom != "" {
				if t, err := stdtime.Parse(stdtime.RFC3339, req.TimeFrom); err == nil {
					opts.TimeFrom = t
				}
			}
			if req.TimeTo != "" {
				if t, err := stdtime.Parse(stdtime.RFC3339, req.TimeTo); err == nil {
					opts.TimeTo = t
				}
			}
			result, err := retrieval.RetrieveContext(env.DB, seedIDs, opts)
			if err != nil {
				return fmt.Errorf("temporal: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}
