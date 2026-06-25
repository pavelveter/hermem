package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
)

func newRollbackCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Roll back the most-recent applied migration",
		Long: `Roll back the most-recent applied migration (default), or use
--target=N to roll back all migrations after the specified version.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, _ := cmd.Flags().GetString("target")
			svc := migration.NewService(env.DB)
			name, err := svc.Rollback(env.Ctx, target)
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			if name == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No migrations.")
			} else if target != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Rolled back to: %s\n", target)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Rolled back: %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().String("target", "", "Roll back to this target version (exclusive)")
	return cmd
}
