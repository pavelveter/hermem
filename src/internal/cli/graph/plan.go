package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/algo"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newPlanCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Render execution plan for a goal (topologically sorted blocked tasks)",
		Args:  cobra.NoArgs,
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
			tasks, err := algo.ExecutionPlan(env.Ctx, env.DB, env.Cfg.Schema, req.GoalID)
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
