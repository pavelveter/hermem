package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/orchestrator"
)

func newLoopCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "loop",
		Short: "Run agent execution loop on a goal_id (yields each task to stdout)",
		Long: `Run the autonomous agent execution loop for a given goal.

Input (JSON on stdin):
  {
    "goal_id": "goal-entity-id"
  }

The agent loop:
  1. Finds all executable tasks (blockers all done) for the goal.
  2. Yields each task to stdout as it is picked up.
  3. Continues until no more executable tasks remain.

Each yielded task is printed as:
  [task-id] content  [category]

This command is designed to be used as a subprocess by an external
orchestrator (e.g., a shell script or AI agent) that reads tasks from
stdout, executes them, and updates their status via "hermem task status".

Requires a running database (or DB path via config).

Examples:
  echo '{"goal_id":"g1"}' | hermem agent loop
  echo '{"goal_id":"g1"}' | hermem agent loop | while read -r line; do
    echo "Processing: $line"
  done`,
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
			slog.Info("agent loop started", "goal_id", req.GoalID)
			svc := orchestrator.New(env.DB)
			err := svc.AgentLoop(env.Ctx, env.Cfg.Schema, req.GoalID,
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
