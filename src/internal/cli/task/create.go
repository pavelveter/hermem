package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
)

func newCreateCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a task (auto-embeds content and assigns the first stateful category)",
		Long: `Create a new task entity in the knowledge graph.

Input (JSON on stdin):
  {
    "id":          "optional-id",        // auto-generated if omitted
    "content":     "task description",
    "context_ids": ["related-entity"]    // optional, links to existing entities
  }

The task is automatically:
  - Assigned a unique ID if none provided
  - Embedded using the configured embedder
  - Stored with the "task" category
  - Linked to any provided context_ids via "context_of" edges

Output:
  {"status":"ok"}

Examples:
  echo '{"content":"Implement user authentication"}' | hermem task create
  echo '{"content":"Write tests","context_ids":["g1"]}' | hermem task create`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskCreateRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Content == "" {
				return fmt.Errorf("content required")
			}
			if req.ID == "" {
				req.ID = core.NewTaskID()
			}
			svc := taskdomain.New(env.DB, env.Embedder, env.VI)
			// Service.Create handles embed + store + context_id edges +
			// auto-link internally. CLI behavior drift in :
			// this command returned errors verbatim from
			// embed / StoreEntityWithEmbedding. the
			// domain wraps everything in "create: <err>" prefix; the
			// shape mirrors HTTP shell's 500 envelope so terminal
			// surface stays consistent across transports.
			if _, err := svc.Create(env.Ctx, req.ID, req.Content, req.ContextIDs, env.Cfg.Schema); err != nil {
				return fmt.Errorf("create: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
