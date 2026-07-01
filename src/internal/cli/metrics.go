package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

// newMetricsCmd writes the Prometheus exposition text to stdout. Replaces
// the old flat `hermem metrics`. env.Metrics.WriteExposition reads from
// the Env-captured atomic.Int64 counters (the same fields the server's
// request handlers bump) and writes Prometheus exposition format.
//
// No value is returned by WriteExposition; cobra returns nil after.
//
// PersistentPreRunE is set to noopPreRun for symmetry with `version`:
// this subcommand doesn't need DB access and shouldn't trigger the
// migrations / vector-index / worker setup that the parent's EnsureDB
// would do.
func newMetricsCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:               "metrics",
		Short:             "Prometheus exposition (mirrors GET /metrics)",
		Long:              "Print Prometheus-format metrics to stdout.\nMirrors the GET /metrics HTTP endpoint. Useful for local debugging\nor piping to prometheus-file-sd without running the HTTP server.",
		Args:              cobra.NoArgs,
		PersistentPreRunE: noopPreRun,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env.Metrics.WriteExposition(cmd.OutOrStdout())
			return nil
		},
	}
}
