package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/reembed"
)

func newReEmbedCmd(env *cli.Env) *cobra.Command {
	var (
		batchSize int
		model     string
	)
	cmd := &cobra.Command{
		Use:   "re-embed",
		Short: "Re-embed all entities (use after model change)",
		Long: `Re-embed all entities using the currently configured embedder.

This command is needed when you change the embedding model (e.g., switch
from a smaller to a larger model, or change providers). After re-embedding,
vector search quality may improve (or change) because all vectors are now
in the same embedding space.

Flags:
  --batch-size    Entities per embedding batch (default 50)
  --model         Override embedder model (empty = keep current)

The command reports progress: re-embedded count, failures, batches,
and elapsed time. Failed entities are logged to stderr.

⚠ Re-embedding a large database can take minutes to hours depending
on entity count and embedder latency. Consider running during off-peak
hours.

Output:
  Re-embed: 1500/1500 entities (failed=0, batches=30, elapsed=45s)

Examples:
  hermem memory re-embed
  hermem memory re-embed --batch-size 100
  hermem memory re-embed --model text-embedding-3-large`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if batchSize <= 0 {
				batchSize = 50
			}
			// PHASE 3.6: ReEmbedAll moved from algo.ReEmbedAll to
			// reembed.Service.ReEmbedAll. Inline construction
			// follows the PHASE 3.4 + 3.5 CLI precedent.
			svc := reembed.New(env.DB, env.VI, env.Embedder)
			result, err := svc.ReEmbedAll(env.Ctx, env.Cfg.VectorDim, batchSize, model)
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
