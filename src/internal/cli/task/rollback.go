package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newRollbackCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Find the rollback task for a given task (companion recovery task)",
		Long: `Find the rollback (recovery) task associated with a given task.

Input (JSON on stdin):
  {
    "id": "task-entity-id"
  }

When a task is rolled back, a companion "rollback" task is automatically
created that records the undo action. This command returns the ID of
that companion task.

If the task has not been rolled back, the response will contain an
empty rollback_task_id.

Output (JSON):
  {"rollback_task_id": "rollback-task-id-or-empty"}

Examples:
  echo '{"id":"t1"}' | hermem task rollback
  echo '{"id":"t1"}' | hermem task rollback | jq -r '.rollback_task_id'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskRollbackRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			rollbackID, err := svc.Rollback(env.Ctx, req.ID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskRollbackResponse{RollbackTaskID: rollbackID})
		},
	}
}
