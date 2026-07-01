package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newShowCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show one task with its blocked-by and recovers-via relations",
		Long: `Display detailed information about a single task, including its relations.

Input (JSON on stdin):
  {
    "id": "task-entity-id"
  }

Output (JSON):
  {
    "entity":      { ... },              // full task entity
    "blocked_by":  ["task-id", ...],     // tasks that block this one
    "recovers_via": ["task-id", ...]     // rollback companion tasks
  }

Use "hermem task tree" to see the full dependency tree under a goal.

Examples:
  echo '{"id":"t1"}' | hermem task show
  echo '{"id":"t1"}' | hermem task show | jq '.blocked_by'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskShowRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			showResult, err := svc.Show(env.Ctx, req.ID, env.Cfg.Schema)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskShowResponse{
				Entity:      showResult.Task,
				BlockedBy:   showResult.BlockedBy,
				RecoversVia: showResult.RecoversVia,
			})
		},
	}
}
