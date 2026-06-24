package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newShowCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show one task with its blocked-by and recovers-via relations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskShowRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			entity, blocked, recovers, err := store.GetTaskWithRelations(env.DB, env.Cfg.Schema, req.ID)
			if err != nil {
				return fmt.Errorf("show: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskShowResponse{
				Entity:      entity,
				BlockedBy:   blocked,
				RecoversVia: recovers,
			})
		},
	}
}
