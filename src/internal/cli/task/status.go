package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newStatusCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Update a task's status (e.g. /* done */ / *blocked*)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.TaskStatusRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" || req.Status == "" {
				return fmt.Errorf("id, status required")
			}
			if err := store.SetStatus(env.DB, env.Cfg.Schema, req.ID, req.Status); err != nil {
				return fmt.Errorf("status: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
