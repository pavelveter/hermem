package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newTreeCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "tree",
		Short: "Render task tree (ASCII) under a goal_id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskTreeRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			nodes, err := store.GetTaskTree(env.DB, env.Cfg.Schema, req.GoalID)
			if err != nil {
				return fmt.Errorf("tree: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), store.RenderTaskTree(nodes, ""))
			return nil
		},
	}
}
