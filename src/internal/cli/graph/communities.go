package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	graphsvc "github.com/pavelveter/hermem/src/internal/graph"
)

func newCommunitiesCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "communities",
		Short: "Detect communities and report global modularity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// PHASE 3.1: routes through the transport-agnostic graph
			// Service. minSize filtering is intentionally NOT done here
			// — Communities returns the unfiltered list to match the
			// domain contract; CLI currently doesn't filter either,
			// keeping pre-PHASE-3.1 behavior identical.
			svc := graphsvc.NewService(env.DB)
			comms, globalQ, err := svc.Communities(env.Ctx, 50)
			if err != nil {
				return fmt.Errorf("communities: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Global modularity: %.6f\n", globalQ)
			for _, c := range comms {
				fmt.Fprintf(out, "[%s] size=%d modularity=%.6f\n", c.ID, c.Size, c.Modularity)
			}
			return nil
		},
	}
}
