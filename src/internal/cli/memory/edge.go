package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
)

func newEdgeCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "edge",
		Short: "Add or auto-create a relation edge between two entities (--auto-create creates missing endpoints)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.EdgeRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			// Pre-validation kept at CLI for verbatim message parity.
			if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
				return fmt.Errorf("source_id, target_id, relation_type required")
			}
			if err := env.Cfg.ValidateRelation(req.RelationType); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			memSvc := memdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
			if err := memSvc.AddEdge(env.Ctx, req, env.Cfg.Schema); err != nil {
				return fmt.Errorf("edge: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
