package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/orchestrator"
)

func newPlanCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Render execution plan for a goal (topologically sorted blocked tasks)",
		Long: `Render a topologically sorted execution plan for a goal's task graph.

Input (JSON on stdin):
  {
    "goal_id": "goal-entity-id"
  }

Tasks are sorted so that blockers always appear before the tasks they
block. Tasks at the same dependency level are listed in arbitrary order.

Output (text, one task per line):
  [task-id] content  [status]

Use "hermem task tree" for a hierarchical tree view instead.

Examples:
  echo '{"goal_id":"g1"}' | hermem graph plan
  echo '{"goal_id":"g1"}' | hermem graph plan | grep '\[pending\]'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				GoalID string `json:"goal_id"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.GoalID == "" {
				return fmt.Errorf("goal_id required")
			}
			svc := orchestrator.New(env.DB)
			tasks, err := svc.ExecutionPlan(env.Ctx, env.Cfg.Schema, req.GoalID)
			if err != nil {
				return fmt.Errorf("plan: %w", err)
			}
			for _, t := range tasks {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s  [%s]\n", t.ID, t.Content, t.Status)
			}
			return nil
		},
	}
}
