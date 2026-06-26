package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newListCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tasks filtered by status and/or goal_id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskListRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			tasks, err := svc.List(env.Ctx, req.Status, req.GoalID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskExecutableResponse{Tasks: tasks})
		},
	}
}
