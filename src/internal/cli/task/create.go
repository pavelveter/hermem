package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newCreateCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a task (auto-embeds content and assigns the first stateful category)",
		Args:  cobra.NoArgs,
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
			emb, err := env.Embedder.Embed(env.Ctx, req.Content)
			if err != nil {
				return fmt.Errorf("embed: %w", err)
			}
			cat := config.FirstStatefulCategory(env.Cfg.Schema)
			if cat == "" {
				return fmt.Errorf("no stateful category configured")
			}
			entity := core.Entity{
				ID:        req.ID,
				Category:  cat,
				Content:   req.Content,
				Embedding: emb,
			}
			if err := store.StoreEntityWithEmbedding(env.DB, env.VI, env.Cfg.Schema, entity); err != nil {
				return fmt.Errorf("store: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
