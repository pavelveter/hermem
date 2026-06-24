package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

func init() { Register("timeline", cliTimeline) }

func cliTimeline(env Env) {
	rows, err := env.DB.QueryContext(env.Ctx, `SELECT id, category, content, created_at FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, cat, content string
		var ts sql.NullTime
		_ = rows.Scan(&id, &cat, &content, &ts)
		fmt.Printf("[%s] %s  %s  [%s]\n", ts.Time.Format(time.RFC3339), id, content, cat)
	}
}
