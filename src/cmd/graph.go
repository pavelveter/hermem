package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/store"
)

// --- graph-algo-* subcommands: execution-plan, recovery-plan,
// connected-components, communities — all read-only batch analytics. ---

func init() {
	Register("execution-plan", cliExecutionPlan)
	Register("recovery-plan", cliRecoveryPlan)
	Register("connected-components", cliConnectedComponents)
	Register("communities", cliCommunities)
}

func cliExecutionPlan(env Env) {
	var req struct {
		GoalID string `json:"goal_id"`
	}
	DecodeStdin(&req)
	if req.GoalID == "" {
		log.Fatal("goal_id required")
	}
	tasks, err := algo.ExecutionPlan(env.DB, env.Cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("plan: %v", err)
	}
	for _, t := range tasks {
		fmt.Printf("[%s] %s  [%s]\n", t.ID, t.Content, t.Status)
	}
}

func cliRecoveryPlan(env Env) {
	var req struct {
		ID string `json:"id"`
	}
	DecodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	plan, err := store.GenerateRecoveryPlan(env.DB, env.Cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("recovery: %v", err)
	}
	for i, t := range plan {
		fmt.Printf("%d. [%s] %s  [%s]\n", i+1, t.ID, t.Content, t.Status)
	}
}

func cliConnectedComponents(env Env) {
	components, err := store.FindConnectedComponents(env.DB, 2)
	if err != nil {
		log.Fatalf("components: %v", err)
	}
	for _, c := range components {
		fmt.Printf("Component (size=%d, avg_degree=%.1f): %v\n", c.Size, c.AvgDegree, c.IDs)
	}
}

func cliCommunities(env Env) {
	comms, globalQ, err := store.DetectCommunities(env.DB, 50)
	if err != nil {
		log.Fatalf("communities: %v", err)
	}
	fmt.Printf("Global modularity: %.6f\n", globalQ)
	for _, c := range comms {
		fmt.Printf("[%s] size=%d modularity=%.6f\n", c.ID, c.Size, c.Modularity)
	}
}
