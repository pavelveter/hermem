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
		Long: `Create a directed relation edge between two entities in the knowledge graph.

Input (JSON on stdin):
  {
    "source_id":      "entity-a",
    "target_id":      "entity-b",
    "relation_type":  "supports|contradicts|extends|...",
    "auto_create":    false              // optional, create missing endpoints
  }

Relation types must match the configured schema (hermem.ini [schema]).
Use "hermem admin config" to see valid relation types.

If "auto_create" is true, missing entities are created as empty shells
(category="world", content=entity-id). This is useful for building
graph structure before filling in content.

Output:
  {"status":"ok"}

Examples:
  echo '{"source_id":"a","target_id":"b","relation_type":"supports"}' | hermem memory edge
  echo '{"source_id":"a","target_id":"c","relation_type":"extends","auto_create":true}' | hermem memory edge`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.EdgeRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
				return fmt.Errorf("source_id, target_id, relation_type required")
			}
			if err := env.Cfg.ValidateRelation(req.RelationType); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			// Build the new domain Service inline per call (cheap
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
