package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/algo"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newReEmbedCmd(env *cli.Env) *cobra.Command {
	var (
		batchSize int
		model     string
	)
	cmd := &cobra.Command{
		Use:   "re-embed",
		Short: "Re-embed all entities (use after model change)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if batchSize <= 0 {
				batchSize = 50
			}
			result, err := algo.ReEmbedAll(env.Ctx, env.DB, env.VI, env.Embedder, env.Cfg.VectorDim, batchSize, model)
			if err != nil {
				return fmt.Errorf("re-embed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Re-embed: %d/%d entities (failed=%d, batches=%d, elapsed=%s)\n",
				result.ReEmbedded, result.TotalEntities, result.Failed, result.Batches, result.Elapsed)
			return nil
		},
	}
	cmd.Flags().IntVar(&batchSize, "batch-size", 50, "entities per embedding batch")
	cmd.Flags().StringVar(&model, "model", "", "override embedder model (empty = keep current)")
	return cmd
}
