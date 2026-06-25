package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	edgedomain "github.com/pavelveter/hermem/src/internal/edge"
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
			// PHASE 3.5: AddEdge moved from memory.Service to edge.Service.
			// Construction shape follows the PHASE 3.4 ingest-cli migration
			// precedent: build the new domain Service inline per call (cheap
			// six-pointer assignment) so the CLI plugin doesn't need a new
			// field on cli.Env. Extractor is no longer required (the edge
			// domain has no LLM hook).
			edgeSvc := edgedomain.New(env.DB, env.VI, env.Embedder)
			if err := edgeSvc.AddEdge(env.Ctx, req, env.Cfg.Schema); err != nil {
				return fmt.Errorf("edge: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
