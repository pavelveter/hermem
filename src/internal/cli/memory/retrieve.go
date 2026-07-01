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
		Long: `Retrieve context by walking the knowledge graph from explicit seed IDs.

Input (JSON on stdin):
  {
    "seed_ids": ["entity-id-1", "entity-id-2"],
    "max_depth": 3                       // optional, default from config
  }

Unlike "query", this command does NOT perform vector search — it starts
the graph walk directly from the given seed IDs. This is useful when you
already know which entities are relevant (e.g., from a prior search or
user selection).

The walk follows edges in the knowledge graph, scoring each discovered
entity by vector similarity, recency, and centrality. Results include
world facts, opinions, experiences, and observations with their scores.

Config overrides (from hermem.ini):
  max_depth_ceiling, max_retrieved_nodes, token_budget, ranking weights

Output (JSON):
  {"world_facts":[...], "opinions":[...], "experiences":[...], "observations":[...]}

Examples:
  echo '{"seed_ids":["e1","e2"],"max_depth":2}' | hermem memory retrieve`,
		Args: cobra.NoArgs,
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
				TokenBudget:       env.Cfg.TokenBudget,
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
