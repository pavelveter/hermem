package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newSearchCmd(env *cli.Env) *cobra.Command {
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
			// Construct per-call (three pointer assignments; cheap) so
			// CLI never holds a stale Service pointer between commands.
			svc := retdomain.NewService(env.DB, env.VI, env.Embedder)
			results, err := svc.Search(env.Ctx, req.Query, req.TopK)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
		},
	}
}
