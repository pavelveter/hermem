package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newRecoveryPlanCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "recovery-plan",
		Short: "Render recovery plan for a failed task (RHS-of-relations walk)",
		Long: `Generate a recovery plan for a failed or rolled-back task.

Input (JSON on stdin):
  {
    "id": "task-entity-id"
  }

Walks the right-hand side of rollback/recovery relations to find all
tasks that need to be undone when a task fails. The plan is numbered
in reverse-dependency order (last to undo first).

Output (text, numbered):
  1. [task-id] content  [status]
  2. [task-id] content  [status]

This is used by "hermem task rollback" and the agent loop to determine
which tasks to cascade-rollback.

Examples:
  echo '{"id":"t1"}' | hermem graph recovery-plan
  echo '{"id":"t1"}' | hermem graph recovery-plan | wc -l`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				ID string `json:"id"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			plan, err := store.GenerateRecoveryPlan(env.DB, env.Cfg.Schema, req.ID)
			if err != nil {
				return fmt.Errorf("recovery: %w", err)
			}
			for i, t := range plan {
				fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] %s  [%s]\n", i+1, t.ID, t.Content, t.Status)
			}
			return nil
		},
	}
}
