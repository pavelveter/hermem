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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskShowRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			svc := taskdomain.NewService(env.DB, env.Embedder, env.VI)
			entity, blocked, recovers, err := svc.Show(env.Ctx, req.ID, env.Cfg.Schema)
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
