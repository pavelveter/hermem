package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
)

func newStoreCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "store",
		Short: "Store an entity (JSON stdin: id/category/content + optional embedding)",
		Long: `Store a single entity in the knowledge graph.

Input (JSON on stdin):
  {
    "id":       "unique-entity-id",
    "category": "world|opinion|experience|observation|task",
    "content":  "free-text content",
    "embedding": [0.1, 0.2, ...]          // optional
  }

If "embedding" is omitted and an embedder is configured, the content is
automatically embedded before storage. If embedding fails, the entity is
stored without a vector (a warning is printed to stderr).

Categories must match the configured schema (hermem.ini [schema]).
Use "hermem admin config" to see valid categories.

Output:
  {"status":"ok"}

Examples:
  echo '{"id":"e1","category":"world","content":"Go is statically typed"}' | hermem memory store
  hermem memory ingest < dialog.txt   # bulk store via ingest`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.StoreRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" || req.Category == "" || req.Content == "" {
				return fmt.Errorf("id, category, content required")
			}
			if err := env.Cfg.ValidateCategory(req.Category); err != nil {
				return fmt.Errorf("invalid: %w", err)
			}
			if len(req.Embedding) == 0 && env.Embedder != nil {
				emb, err := env.Embedder.Embed(env.Ctx, req.Content)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: embed failed (%v), storing without embedding\n", err)
				} else {
					req.Embedding = emb
				}
			}
			// Construct per-call (three pointer assignments; cheap) so
			// CLI never holds onto a stale Service ref between commands.
			memSvc := memdomain.New(env.DB, env.VI, env.Embedder)
			if err := memSvc.Store(env.Ctx, req, env.Cfg.Schema); err != nil {
				return fmt.Errorf("store: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
