package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// newMetricsCmd writes the Prometheus exposition text to stdout. Replaces
// the old flat `hermem metrics`. metrics.WriteExposition returns no value
// (it's a side-effecting writer), so we disregard its result and let
// cobra return nil after.
func newMetricsCmd(env clienv.Env) *cobra.Command {
	_ = env
	return &cobra.Command{
		Use:   "metrics",
		Short: "Prometheus exposition (mirrors GET /metrics)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			metrics.WriteExposition(cmd.OutOrStdout())
			return nil
		},
	}
}
