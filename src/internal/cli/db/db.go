// Package db hosts the database housekeeping commands.
//
//	hermem db <sub>       # migrate / rollback / verify / schema
package db

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the db group cobra command.
func NewCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database housekeeping (migrate / rollback / verify / schema)",
	}
	cmd.AddCommand(
		newMigrateCmd(env),
		newRollbackCmd(env),
		newVerifyCmd(env),
		newSchemaCmd(env),
		newDryRunCmd(env),
	)
	return cmd
}
