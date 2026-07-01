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
		Long: `Compare the current schema fingerprint against the stored DB fingerprint.

The schema fingerprint is a hash of the expected database structure
(derived from the migration files). The stored fingerprint is what
the DB reports about its current state.

Output (text):
  Current: abc123...
  Stored:  def456...
  WARNING: schema changed!

If "WARNING: schema changed!" appears, the on-disk migrations have
produced a different schema than what the DB currently has. Run
"hermem db migrate apply" to bring the DB up to date.

Examples:
  hermem db schema
  hermem db schema | grep -q WARNING && echo "needs migration"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// PHASE 3.2: routes through migration.Service.Schema
			// (transport-agnostic). Schema is fetched per call from
			// env.Cfg so a SIGHUP-driven cfg Schema swap applies
			// without reconstructing the service.
			svc := migration.New(env.DB)
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
