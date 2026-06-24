package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/core"
)

func init() { Register("agent-loop", cliAgentLoop) }

func cliAgentLoop(env Env) {
	var req struct {
		GoalID string `json:"goal_id"`
	}
	DecodeStdin(&req)
	if req.GoalID == "" {
		log.Fatal("goal_id required")
	}
	slog.Info("agent loop started", "goal_id", req.GoalID)
	err := algo.AgentLoop(env.Ctx, env.DB, env.Cfg.Schema, req.GoalID, func(_ context.Context, task core.Entity) error {
		fmt.Printf("[%s] %s  [%s]\n", task.ID, task.Content, task.Category)
		return nil
	})
	if err != nil {
		log.Fatalf("agent loop: %v", err)
	}
}
