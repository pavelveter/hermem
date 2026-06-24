package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newComponentsCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "Find connected components in the graph (size ≥ 2)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			components, err := store.FindConnectedComponents(env.DB, 2)
			if err != nil {
				return fmt.Errorf("components: %w", err)
			}
			for _, c := range components {
				fmt.Fprintf(cmd.OutOrStdout(), "Component (size=%d, avg_degree=%.1f): %v\n",
					c.Size, c.AvgDegree, c.IDs)
			}
			return nil
		},
	}
}
