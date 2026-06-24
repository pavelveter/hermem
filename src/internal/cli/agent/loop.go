package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/algo"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
)

func newLoopCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "loop",
		Short: "Run agent execution loop on a goal_id (yields each task to stdout)",
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
			slog.Info("agent loop started", "goal_id", req.GoalID)
			err := algo.AgentLoop(env.Ctx, env.DB, env.Cfg.Schema, req.GoalID,
				func(_ context.Context, task core.Entity) error {
					fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s  [%s]\n",
						task.ID, task.Content, task.Category)
					return nil
				})
			if err != nil {
				return fmt.Errorf("agent loop: %w", err)
			}
			return nil
		},
	}
}
