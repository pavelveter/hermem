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
		Long: `Render the task dependency tree as an ASCII diagram.

Input (JSON on stdin):
  {
    "goal_id": "goal-entity-id"
  }

Prints a tree showing the goal at the root, with all child tasks
and their dependency chains. Each node shows the task ID, content
(truncated), and status in brackets.

Example output:
  [g1] Build feature X  [done]
    ├─ [t1] Design API  [done]
    ├─ [t2] Implement backend  [done]
    │  └─ [t3] Write tests  [pending]
    └─ [t4] Deploy  [blocked]

Use "hermem graph plan" for a topologically sorted execution order
instead of a tree view.

Examples:
  echo '{"goal_id":"g1"}' | hermem task tree
  echo '{"goal_id":"g1"}' | hermem task tree | head -20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskTreeRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			tree, err := svc.Tree(env.Ctx, req.GoalID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("tree: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), tree)
			return nil
		},
	}
}
