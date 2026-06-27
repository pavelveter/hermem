package adminops

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/admin"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newRebuildIndexCmd(env *cli.Env) *cobra.Command {
	var (
		category     string
		sinceStr     string
		onlyArchived bool
		dryRun       bool
	)
	cmd := &cobra.Command{
		Use:   "rebuild-index",
		Short: "Re-build vector index for filtered entities",
		Long: `Re-generates embeddings and re-indexes entities matching the filter.
Use --dry-run to preview without changes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vi, ok := env.VI.(admin.VectorIndex)
			if !ok || vi == nil {
				return fmt.Errorf("vector index not available (VI is nil or does not implement admin.VectorIndex)")
			}
			em := env.Embedder
			if em == nil {
				return fmt.Errorf("embedder not available")
			}

			var since time.Time
			if sinceStr != "" {
				var err error
				since, err = time.Parse("2006-01-02", sinceStr)
				if err != nil {
					return fmt.Errorf("invalid --since format (use YYYY-MM-DD): %w", err)
				}
			}

			ri := admin.NewRebuildIndex(env.DB, vi, em)
			ri.OnLog(func(msg string) {
				fmt.Fprintln(cmd.ErrOrStderr(), msg)
			})

			report, err := ri.Run(cmd.Context(), admin.RebuildOpts{
				Category:     category,
				Since:        since,
				OnlyArchived: onlyArchived,
				DryRun:       dryRun,
			})
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would re-embed %d entities (dry run)\n", report.Processed)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Processed: %d, Re-embedded: %d, Failed: %d\n",
				report.Processed, report.Reembedded, report.Failed)
			for _, e := range report.Errors {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", e)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "only entities matching this category")
	cmd.Flags().StringVar(&sinceStr, "since", "", "only entities updated since this date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&onlyArchived, "only-archived", false, "only archived entities")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without making changes")
	return cmd
}
