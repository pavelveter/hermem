package db

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
)

// newVerifyCmd checks migration checksums. Pre-cobra called os.Exit(1)
// on integrity mismatch; now we return an error and main.go handles exit
// code 1, so the failure path is no longer a hidden syscall.
//
// PHASE 3.2: routes through migration.Service.Verify.
func newVerifyCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify migration checksums (exit 1 on mismatch)",
		Long: `Verify that on-disk migration files match their applied checksums.

Each migration file has a SHA-256 checksum computed at apply time. This
command recomputes the checksum from the current on-disk file and
compares it to the stored value.

Output (text):
  All migration checksums intact.    (exit 0)
  OR
  N checksum mismatch(es):           (exit 1)
    0002_add_edges.sql
      stored:   abc123...
      current:  def456...

Exit codes:
  0  all checksums match
  1  mismatch detected (possible file tampering or accidental edit)

Run this in CI or as a pre-deploy check to catch unauthorized schema
changes.

Examples:
  hermem db verify
  hermem db verify || echo "migration files tampered"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := migration.New(env.DB)
			mismatches, err := svc.Verify(env.Ctx)
			if err != nil {
				return fmt.Errorf("verify: %w", err)
			}
			if len(mismatches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "All migration checksums intact.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d checksum mismatch(es):\n", len(mismatches))
			for _, mm := range mismatches {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", mm.Name)
				fmt.Fprintf(cmd.OutOrStdout(), "    stored:   %s\n", mm.StoredChecksum)
				fmt.Fprintf(cmd.OutOrStdout(), "    current:  %s\n", mm.CurrentChecksum)
			}
			fmt.Fprintln(os.Stderr, "db verify: integrity mismatch")
			return fmt.Errorf("migration integrity mismatch: %d", len(mismatches))
		},
	}
}
