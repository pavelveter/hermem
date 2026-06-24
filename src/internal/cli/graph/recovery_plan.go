package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newRecoveryPlanCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "recovery-plan",
		Short: "Render recovery plan for a failed task (RHS-of-relations walk)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				ID string `json:"id"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.ID == "" {
				return fmt.Errorf("id required")
			}
			plan, err := store.GenerateRecoveryPlan(env.DB, env.Cfg.Schema, req.ID)
			if err != nil {
				return fmt.Errorf("recovery: %w", err)
			}
			for i, t := range plan {
				fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] %s  [%s]\n", i+1, t.ID, t.Content, t.Status)
			}
			return nil
		},
	}
}
