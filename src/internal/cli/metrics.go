package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// newMetricsCmd writes the Prometheus exposition text to stdout. Replaces
// the old flat `hermem metrics`. metrics.WriteExposition returns no value
// (it's a side-effecting writer that reads package-level in-memory
// counters only — no DB access), so we disregard its return and let
// cobra return nil after.
//
// PersistentPreRunE is set to noopPreRun for symmetry with `version`:
// this subcommand doesn't need DB access and shouldn't trigger the
// migrations / vector-index / worker setup that the parent's EnsureDB
// would do.
func newMetricsCmd(env *clienv.Env) *cobra.Command {
	_ = env
	return &cobra.Command{
		Use:               "metrics",
		Short:             "Prometheus exposition (mirrors GET /metrics)",
		Args:              cobra.NoArgs,
		PersistentPreRunE: noopPreRun,
		RunE: func(cmd *cobra.Command, _ []string) error {
			metrics.WriteExposition(cmd.OutOrStdout())
			return nil
		},
	}
}
