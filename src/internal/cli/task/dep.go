package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newDepCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "dep",
		Short: "Add or remove a blocking dependency between two tasks (body {add:true|false})",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskDepRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.SourceID == "" || req.TargetID == "" {
				return fmt.Errorf("source_id, target_id required")
			}
			rel := req.RelationType
			if rel == "" {
				rel = env.Cfg.Schema.RelationBlocking
			}
			if err := env.Cfg.ValidateRelation(rel); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			if req.Add {
				if err := store.AddEdge(env.DB, req.SourceID, req.TargetID, rel, 1.0); err != nil {
					return fmt.Errorf("add: %w", err)
				}
			} else {
				_ = store.DeleteEdge(env.DB, req.SourceID, req.TargetID, rel)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
