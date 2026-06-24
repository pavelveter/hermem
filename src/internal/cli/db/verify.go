package db

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

// newVerifyCmd checks migration checksums. Pre-cobra called os.Exit(1)
// on integrity mismatch; now we return an error and main.go handles exit
// code 1, so the failure path is no longer a hidden syscall.
func newVerifyCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify migration checksums (exit 1 on mismatch)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mismatches, err := store.VerifyMigrationIntegrity(env.DB)
			if err != nil {
				return fmt.Errorf("verify: %w", err)
			}
			if len(mismatches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "All migration checksums intact.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d mismatch(es)\n", len(mismatches))
			fmt.Fprintln(os.Stderr, "db verify: integrity mismatch")
			return fmt.Errorf("migration integrity mismatch: %d", len(mismatches))
		},
	}
}
