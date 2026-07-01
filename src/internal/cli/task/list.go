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
		Long: `List tasks with optional filters.

Input (JSON on stdin):
  {
    "status": "pending",                 // optional, filter by status
    "goal_id": "goal-entity-id"          // optional, filter by goal
  }

Both filters are optional. Omitting both lists all tasks.

Output (JSON):
  {"tasks": [{"id":"t1", "content":"...", "status":"pending", ...}, ...]}

Examples:
  echo '{}' | hermem task list
  echo '{"status":"pending"}' | hermem task list
  echo '{"goal_id":"g1","status":"done"}' | hermem task list | jq '.tasks | length'`,
		Args: cobra.NoArgs,
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
