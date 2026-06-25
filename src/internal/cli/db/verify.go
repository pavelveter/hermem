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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := migration.NewService(env.DB)
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
