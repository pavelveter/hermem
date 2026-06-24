package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newRollbackCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Find the rollback task for a given task (companion recovery task)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskRollbackRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			rollbackID, err := store.FindRollbackTask(env.DB, env.Cfg.Schema, req.ID)
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskRollbackResponse{RollbackTaskID: rollbackID})
		},
	}
}
