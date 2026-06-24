package task

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
)

// newExecutableCmd lists tasks whose blockers are all done. Cobra exposes
// it under both `task executable` and the friendlier alias `task next`.
// Empty stdin is silently substituted with "{}" so the command runs when
// invoked from a shell without piping JSON.
func newExecutableCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "executable",
		Aliases: []string{"next"},
		Short:   "List currently-executable tasks (blockers all done). Aliases: next",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := cli.ReadStdin()
			if err != nil && err != cli.ErrStdinRequired {
				return err
			}
			if data == "" {
				data = "{}"
			}
			var req struct {
				GoalID string `json:"goal_id"`
			}
			if err := cli.DecodeString(data, &req); err != nil {
				return err
			}
			tasks, err := retrieval.GetExecutableTasks(env.DB, env.Cfg.Schema, req.GoalID)
			if err != nil {
				return fmt.Errorf("executable: %w", err)
			}
			if tasks == nil {
				tasks = []core.Entity{}
			}
			return cli.WriteJSON(cmd.OutOrStdout(), core.TaskExecutableResponse{Tasks: tasks})
		},
	}
}
