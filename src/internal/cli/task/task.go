// Package task hosts the task lifecycle commands.
//
//	hermem task <sub>     # status / list / show / dep / tree / create /
//	                     # rollback / next (alias for executable)
//
// All subcommands consume JSON from stdin except `next` which silently
// falls back to "{}" when stdin is empty so it can run unpipe'd.
package task

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the task group cobra command. Attach it under the root
// to expose `hermem task <sub>`.
func NewCmd(env cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task lifecycle (status / list / show / dep / tree / create / rollback / next)",
	}
	cmd.AddCommand(
		newStatusCmd(env),
		newListCmd(env),
		newShowCmd(env),
		newDepCmd(env),
		newTreeCmd(env),
		newCreateCmd(env),
		newRollbackCmd(env),
		newExecutableCmd(env),
	)
	return cmd
}
