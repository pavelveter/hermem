package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newSchemaCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Show schema fingerprint (current vs stored in DB)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stored, current, err := store.CheckSchemaFingerprint(env.DB, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("schema: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Current: %s\nStored:   %s\n", current, stored)
			if stored != "" && stored != current {
				fmt.Fprintln(out, "WARNING: schema changed!")
			}
			return nil
		},
	}
}
