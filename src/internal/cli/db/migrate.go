package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
)

func newMigrateCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Show migration status (applied / pending)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// PHASE 3.2: routes through the transport-agnostic migration
			// Service rather than hitting store.* directly. Mirrors the
			// PHASE 2.x + 3.1 pattern of "domain service per call".
			svc := migration.NewService(env.DB)
			status, err := svc.Status(env.Ctx)
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
