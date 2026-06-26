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
		Args:  cobra.NoArgs,
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
					return fmt.Errorf("embed: %w", err)
				}
				req.Embedding = emb
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
