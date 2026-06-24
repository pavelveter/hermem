package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newMigrateCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Show migration status (applied / pending)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := store.MigrationStatus(env.DB)
			if err != nil {
				return fmt.Errorf("migrate status: %w", err)
			}
			for _, m := range status {
				mark := "--"
				if m.Applied {
					mark = "OK"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s", mark, m.Name)
				if m.AppliedAt != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  (%s)", m.AppliedAt)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
}
