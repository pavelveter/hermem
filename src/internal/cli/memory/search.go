package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newSearchCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "search",
		Short: "Top-K nearest neighbors by query embedding",
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
				req.TopK = 5
			}
			emb, err := env.Embedder.Embed(env.Ctx, req.Query)
			if err != nil {
				return fmt.Errorf("embed: %w", err)
			}
			results, err := vector.SearchByVector(env.DB, env.VI, emb, req.TopK)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
		},
	}
}
