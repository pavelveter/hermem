// Package time hosts the temporal query commands.
//
//	hermem time <sub>     # temporal / timeline
//
// Both subcommands consume JSON-from-stdin or read directly; timeline
// reads the entities table directly (no embedder / extractor).
package time

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the time group cobra command.
func NewCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "time",
		Short: "Temporal queries (temporal / timeline)",
		Long: `Time-based queries and event timeline: search entities by temporal
context, browse the chronological event timeline, and explore
when facts were created or modified.

Use "hermem time <sub> --help" for request schemas.`,
	}
	cmd.AddCommand(
		newTemporalCmd(env),
		newTimelineCmd(env),
	)
	return cmd
}
