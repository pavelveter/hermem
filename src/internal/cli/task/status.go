package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newStatusCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Update a task's status (e.g. /* done */ / *blocked*)",
		Long: `Update the status of a task entity.

Input (JSON on stdin):
  {
    "id":     "task-entity-id",
    "status": "pending|in_progress|done|blocked|failed|rolled_back"
  }

Valid status transitions are enforced by the schema. Rolling back a
task creates a companion "rollback" task that records the undo action.

Status values:
  pending      — task is waiting to be picked up
  in_progress  — task is being worked on
  done         — task completed successfully
  blocked      — task is blocked by an unfinished dependency
  failed       — task failed
  rolled_back  — task was rolled back (companion task created)

Output:
  {"status":"ok"}

Examples:
  echo '{"id":"t1","status":"done"}' | hermem task status
  echo '{"id":"t2","status":"in_progress"}' | hermem task status`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskStatusRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" || req.Status == "" {
				return fmt.Errorf("id, status required")
			}
			// Construct per-call (six pointer assignments; cheap) so
			// CLI never holds onto a stale Service ref between commands.
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			if err := svc.Status(env.Ctx, req.ID, req.Status, env.Cfg.Schema); err != nil {
				return fmt.Errorf("status: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
