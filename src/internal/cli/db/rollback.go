package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newRollbackCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Roll back the most-recent applied migration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := store.RollbackMigration(env.DB)
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			if name == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No migrations.")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Rolled back: %s\n", name)
			}
			return nil
		},
	}
}
