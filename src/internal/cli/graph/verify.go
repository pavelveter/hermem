package graph

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	graphsvc "github.com/pavelveter/hermem/src/internal/graph"
)

// newVerifyCmd runs graph.Service.Verify. PHASE 3.1 now goes through
// the domain service instead of algo.VerifyGraph directly. The CLI
// retains the !report.Pass() exit-1 logic because that's a CLI-shape
// concern (shell exit codes), not a domain concern.
func newVerifyCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify graph integrity (orbits, embedding dim, FK consistency). Exit 1 on failure.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := graphsvc.New(env.DB)
			// Verify takes (schema, dim) per call so SIGHUP-driven
			// config reloads apply without reconstructing the service.
			report, err := svc.Verify(env.Ctx, env.Cfg.Schema, env.Cfg.VectorDim)
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
