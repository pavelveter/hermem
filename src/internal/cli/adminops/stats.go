package adminops

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/admin"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newStatsCmd(env *cli.Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print DB statistics: entity/edge/contradiction counts + embedding coverage",
		Long: `Print database statistics including entity counts, embedding coverage,
and disk usage.

No input required — reads directly from the database.

Output (text):
  Node count           1523
  Edge count           4891
  Archived count       12
  Contradiction count  3
  Embedding coverage   98.5%
  DB size              45.2 MB
  Last GC run          2024-06-15 10:30:00
  Last GC archived     5
  Captured at          2024-06-20 14:00:00

Flags:
  --json    Output as JSON instead of text table

Examples:
  hermem ops stats
  hermem ops stats --json | jq '.node_count'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := admin.NewStatsCollector(env.DB)
			stats, err := c.Collect(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(stats)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Node count\t%d\n", stats.NodeCount)
			fmt.Fprintf(w, "Edge count\t%d\n", stats.EdgeCount)
			fmt.Fprintf(w, "Archived count\t%d\n", stats.ArchivedCount)
			fmt.Fprintf(w, "Contradiction count\t%d\n", stats.ContradictionCount)
			fmt.Fprintf(w, "Embedding coverage\t%.1f%%\n", stats.EmbeddingCoverage*100)
			fmt.Fprintf(w, "DB size\t%s\n", byteSize(stats.DBSizeBytes))
			if !stats.LastGCRunAt.IsZero() {
				fmt.Fprintf(w, "Last GC run\t%s\n", stats.LastGCRunAt.Format("2006-01-02 15:04:05"))
				fmt.Fprintf(w, "Last GC archived\t%d\n", stats.LastGCArchived)
			}
			fmt.Fprintf(w, "Captured at\t%s\n", stats.CapturedAt.Format("2006-01-02 15:04:05"))
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func byteSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
