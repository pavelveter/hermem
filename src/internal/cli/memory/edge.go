package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newEdgeCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "edge",
		Short: "Add or auto-create a relation edge between two entities (--auto-create creates missing endpoints)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				SourceID     string  `json:"source_id"`
				TargetID     string  `json:"target_id"`
				RelationType string  `json:"relation_type"`
				Weight       float32 `json:"weight,omitempty"`
				AutoCreate   bool    `json:"auto_create,omitempty"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
				return fmt.Errorf("source_id, target_id, relation_type required")
			}
			if err := env.Cfg.ValidateRelation(req.RelationType); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			var edgeErr error
			if req.AutoCreate {
				edgeErr = vector.AddEdgeWithAutoCreate(env.Ctx, env.DB, env.VI, env.Embedder, req.SourceID, req.TargetID, req.RelationType)
			} else {
				edgeErr = store.AddEdge(env.DB, req.SourceID, req.TargetID, req.RelationType, req.Weight)
			}
			if edgeErr != nil {
				return fmt.Errorf("edge: %w", edgeErr)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
