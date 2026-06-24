package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
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
			svc := taskdomain.NewService(env.DB, env.Embedder, env.VI)
			tree, err := svc.Tree(env.Ctx, req.GoalID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("tree: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), tree)
			return nil
		},
	}
}
