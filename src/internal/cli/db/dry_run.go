package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
)

func newDryRunCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "dry-run",
		Short: "Show pending migrations without applying them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := migration.NewService(env.DB)
			pending, err := svc.DryRun(env.Ctx)
			if err != nil {
				return fmt.Errorf("dry-run: %w", err)
			}
			if len(pending) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "All migrations applied. Nothing to dry-run.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d pending migration(s):\n", len(pending))
			for _, m := range pending {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s", m.Name)
				if m.ChecksumSHA256 != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  sha256:%s", m.ChecksumSHA256[:12])
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
}
