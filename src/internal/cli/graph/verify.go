package graph

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/algo"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// newVerifyCmd runs algo.VerifyGraph. Pre-cobra this called os.Exit(1)
// on failure; we now return an error and let main.go exit non-zero — the
// result is identical from a shell's POV but the failure path is now
// type-checked and covered by cobra's error renderer.
func newVerifyCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify graph integrity (orbits, embedding dim, FK consistency). Exit 1 on failure.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := algo.VerifyGraph(env.DB, env.Cfg.Schema, env.Cfg.VectorDim)
			if err != nil {
				return fmt.Errorf("verify: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), report.String())
			if !report.Pass() {
				fmt.Fprintln(os.Stderr, "verify: integrity check failed")
				return fmt.Errorf("integrity check failed")
			}
			return nil
		},
	}
}
