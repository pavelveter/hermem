package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newRetrieveCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "retrieve",
		Short: "Graph-walk retrieval from explicit seed IDs (no embedding step)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.RetrieveRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if len(req.SeedIDs) == 0 {
				return fmt.Errorf("seed_ids required")
			}
			if req.MaxDepth <= 0 {
				req.MaxDepth = retdomain.DefaultRetrieveMaxDepth
			}
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				MaxDepth:          req.MaxDepth,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
				Ctx:               env.Ctx,
			}
			result, err := svc.Retrieve(env.Ctx, req.SeedIDs, opts)
			if err != nil {
				return fmt.Errorf("retrieve: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}
