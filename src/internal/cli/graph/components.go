package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	graphsvc "github.com/pavelveter/hermem/src/internal/graph"
)

func newComponentsCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "Find connected components in the graph (size ≥ 2)",
		Long: `Find connected components in the knowledge graph.

A connected component is a group of entities where every entity is
reachable from every other entity via edges. Only components with
size >= 2 are reported (isolated nodes are excluded).

Output (text, one component per line):
  Component (size=N, avg_degree=D): [id1, id2, ...]

Use "hermem graph communities" for a more granular decomposition that
accounts for edge weights and community structure.

Examples:
  hermem graph components
  hermem graph components | wc -l`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// PHASE 3.1: routes through the transport-agnostic graph
			// Service rather than hitting store.* directly. Mirrors the
			// PHASE 2.x pattern of "domain service per call".
			svc := graphsvc.New(env.DB)
			components, err := svc.Components(env.Ctx, 2)
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
