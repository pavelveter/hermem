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
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
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
