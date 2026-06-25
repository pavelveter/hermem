// Package adminops hosts the offline database operations:
//
//	hermem ops stats             # print DB statistics
//	hermem ops integrity         # run integrity checks
//	hermem ops vacuum            # SQLite VACUUM
//	hermem ops rebuild-index     # re-build vector index for filtered entities
//
// Registered as "ops" (not "admin") to avoid collision with auth key
// management ("hermem admin keys …") added by the auth-hardening sprint.
package adminops

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// Register wires the "ops" command group under parent (the root command).
func Register(parent *cobra.Command, env *cli.Env) {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Offline DB operations: stats, integrity, vacuum, rebuild-index",
		Long:  "Operational commands that work on the database directly — no HTTP server required.",
	}
	cmd.AddCommand(
		newStatsCmd(env),
		newIntegrityCmd(env),
		newVacuumCmd(env),
		newRebuildIndexCmd(env),
	)
	parent.AddCommand(cmd)
}
