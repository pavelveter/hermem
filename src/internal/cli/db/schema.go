package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
)

func newSchemaCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Show schema fingerprint (current vs stored in DB)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// PHASE 3.2: routes through migration.Service.Schema
			// (transport-agnostic). Schema is fetched per call from
			// env.Cfg so a SIGHUP-driven cfg Schema swap applies
			// without reconstructing the service.
			svc := migration.NewService(env.DB)
			report, err := svc.Schema(env.Ctx, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("schema: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Current: %s\nStored:   %s\n", report.Current, report.Stored)
			if report.DriftDetected {
				fmt.Fprintln(out, "WARNING: schema changed!")
			}
			return nil
		},
	}
}
