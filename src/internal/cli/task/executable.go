package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

// newExecutableCmd lists tasks whose blockers are all done. Cobra exposes
// it under both `task executable` and the friendlier alias `task next`.
// Empty stdin is silently substituted with "{}" so the command runs when
// invoked from a shell without piping JSON.
func newExecutableCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "executable",
		Aliases: []string{"next"},
		Short:   "List currently-executable tasks (blockers all done). Aliases: next",
		Long: `List tasks whose blocking dependencies are all completed.

Input (JSON on stdin):
  {
    "goal_id": "goal-entity-id"          // optional, filter by goal
  }

Empty stdin is accepted (runs as if "{}" was piped).

A task is "executable" when all tasks that block it have status "done".
This is the command an agent loop uses to pick up the next task to work
on.

Output (JSON):
  {"tasks": [{"id":"t1", "content":"...", "status":"pending", ...}, ...]}

Aliases: hermem task next

Examples:
  echo '{}' | hermem task executable
  hermem task next
  echo '{"goal_id":"g1"}' | hermem task next | jq '.tasks[0].id'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := cli.ReadStdin()
			if err != nil && err != cli.ErrStdinRequired {
				return err
			}
			if data == "" {
				data = "{}"
			}
			var req struct {
				GoalID string `json:"goal_id"`
			}
			if err := cli.DecodeString(data, &req); err != nil {
				return err
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			tasks, err := svc.Executable(env.Ctx, req.GoalID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("executable: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskExecutableResponse{Tasks: tasks})
		},
	}
}
