package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
)

func newResponseCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "response",
		Short: "Generate an LLM response using retrieved graph context as evidence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Query    string `json:"query"`
				MaxDepth int    `json:"max_depth,omitempty"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Query == "" {
				return fmt.Errorf("query is required")
			}
			opts := core.RetrieveContextOptions{
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			if req.MaxDepth > 0 {
				opts.MaxDepth = req.MaxDepth
			}
			out, err := retrieval.GenerateResponse(env.Ctx, env.DB, env.VI, env.Embedder, opts, req.Query)
			if err != nil {
				return fmt.Errorf("response: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"response": out})
		},
	}
}
