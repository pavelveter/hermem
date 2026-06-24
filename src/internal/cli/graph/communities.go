package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newCommunitiesCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "communities",
		Short: "Detect communities and report global modularity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			comms, globalQ, err := store.DetectCommunities(env.DB, 50)
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
