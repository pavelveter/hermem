package time

import (
	"database/sql"
	"fmt"
	stdtime "time"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newTimelineCmd(env *cli.Env) *cobra.Command {
	limit := 50
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Most-recent entities (created_at DESC, archived=0)",
		Long: `Show the most recently created entities in the knowledge graph.

No input required — this is a direct database query.

Output (text, one entity per line):
  [RFC3339-timestamp] entity-id  content  [category]

Entities are sorted by created_at descending (newest first). Only
non-archived entities are included.

Use --limit to control how many entities are returned.

Examples:
  hermem time timeline
  hermem time timeline --limit 10
  hermem time timeline | head -5`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 0 {
				return fmt.Errorf("limit must be non-negative")
			}
			rows, err := env.DB.QueryContext(env.Ctx,
				`SELECT id, category, content, created_at FROM entities
				 WHERE archived = 0 AND created_at IS NOT NULL
				 ORDER BY created_at DESC LIMIT ?`, limit)
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
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of entities to return")
	return cmd
}
