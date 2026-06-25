package admin

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

func NewCmd(env *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations (keys, config)",
	}
	cmd.AddCommand(newKeysCmd(env))
	return cmd
}
