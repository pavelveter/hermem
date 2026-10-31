package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	consolidationdomain "github.com/pavelveter/hermem/src/internal/consolidation"
)

func newConsolidateCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "consolidate",
		Short: "Run semantic memory consolidation (graph sleep cycle)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := consolidationdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
			rep, err := svc.Consolidate(env.Ctx)
			if err != nil {
				return fmt.Errorf("consolidate: %w", err)
			}
			b, _ := json.Marshal(rep)
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
}
