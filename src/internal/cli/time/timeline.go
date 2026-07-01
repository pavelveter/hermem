package time

import (
	"database/sql"
	"fmt"
	stdtime "time"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newTimelineCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "timeline",
		Short: "Most-recent 50 entities (created_at DESC, archived=0)",
		Long: `Show the 50 most recently created entities in the knowledge graph.

No input required — this is a direct database query.

Output (text, one entity per line):
  [RFC3339-timestamp] entity-id  content  [category]

Entities are sorted by created_at descending (newest first). Only
non-archived entities are included.

Use this for a quick overview of what's in the graph, or to verify
that recent ingestion worked correctly.

Examples:
  hermem time timeline
  hermem time timeline | head -5`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, err := env.DB.QueryContext(env.Ctx,
				`SELECT id, category, content, created_at FROM entities
				 WHERE archived = 0 AND created_at IS NOT NULL
				 ORDER BY created_at DESC LIMIT 50`)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var id, cat, content string
				var ts sql.NullTime
				if err := rows.Scan(&id, &cat, &content, &ts); err != nil {
					return fmt.Errorf("scan: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s  %s  [%s]\n",
					ts.Time.Format(stdtime.RFC3339), id, content, cat)
			}
			return rows.Err()
		},
	}
}
