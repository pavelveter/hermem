package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newListCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tasks filtered by status and/or goal_id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskListRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			tasks, err := store.ListTasks(env.DB, env.Cfg.Schema, req.Status, req.GoalID)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}
			if tasks == nil {
				tasks = []core.Entity{}
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskExecutableResponse{Tasks: tasks})
		},
	}
}
