package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newDepCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "dep",
		Short: "Add or remove a blocking dependency between two tasks (body {add:true|false})",
		Long: `Manage blocking dependencies between tasks.

Input (JSON on stdin):
  {
    "source_id":      "task-a",          // blocker
    "target_id":      "task-b",          // blocked task
    "relation_type":  "blocks",          // optional, default from schema
    "add":            true               // true=add dependency, false=remove
  }

When "add" is true, task-a becomes a blocker for task-b. Task-b cannot
be executed until task-a is done. When "add" is false, the dependency
is removed (task-b becomes unblocked if this was its only blocker).

The relation_type defaults to the schema's configured blocking relation.
Duplicate adds are silently ignored (no-op).

Output:
  {"status":"ok"}

Examples:
  echo '{"source_id":"t1","target_id":"t2","add":true}' | hermem task dep
  echo '{"source_id":"t1","target_id":"t2","add":false}' | hermem task dep`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskDepRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.SourceID == "" || req.TargetID == "" {
				return fmt.Errorf("source_id, target_id required")
			}
			rel := req.RelationType
			if rel == "" {
				rel = env.Cfg.Schema.RelationBlocking
			}
			if err := env.Cfg.ValidateRelation(rel); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			// CLI behavior drift in PHASE 2.4: pre-PHASE-2.4 `task dep`
			// returned an error on duplicate-edge AddEdge failures
			// ("add: %w"). Post-PHASE-2.4 Service.Dep matches the HTTP
			// shell's pre-PHASE-2.4 behavior of swallowing AddEdge
			// errors so duplicate adds become no-ops rather than
			// surfaced 500s. The ValidateRelation pre-check above is
			// preserved so an unknown relation_type still errors out.
			if err := svc.Dep(env.Ctx, req.SourceID, req.TargetID, rel, req.Add); err != nil {
				return fmt.Errorf("dep: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
