package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

// newContradictionsCmd — single optional positional [entity-id] narrows
// the result. cobra.MaximumNArgs(1) gives the same semantic validation
// the pre-cobra os.Args[2] read did, but as a first-class CLI contract.
func newContradictionsCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "contradictions [entity-id]",
		Short: "List contradictions (optional entity-id narrows the query)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			pairs, err := store.GetContradictions(env.DB, id)
			if err != nil {
				return fmt.Errorf("contradictions: %w", err)
			}
			for _, p := range pairs {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n  contradicts [%s] %s\n\n",
					p.SourceID, p.SourceContent, p.TargetID, p.TargetContent)
			}
			return nil
		},
	}
}
