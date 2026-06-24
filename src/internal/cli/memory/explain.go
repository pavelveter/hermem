package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newExplainCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "explain",
		Short: "Explain the reasoning path from query to retrieved entities",
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
			svc := retdomain.NewService(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				QueryText:         req.Query,
				Ctx:               env.Ctx,
				Explain:           true,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			result, err := svc.Explain(env.Ctx, req.Query, req.TopK, opts)
			if err != nil {
				return fmt.Errorf("explain: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}
