package adminops

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/admin"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newIntegrityCmd(env *cli.Env) *cobra.Command {
	var jsonOut bool
	var failOnWarning bool
	cmd := &cobra.Command{
		Use:   "integrity",
		Short: "Run database integrity checks",
		Long: `Run SQLite-level and application-level integrity checks on the database.

Checks performed:
  - SQLite page integrity (PRAGMA integrity_check)
  - Entity table consistency
  - Edge foreign key validity
  - Embedding dimension consistency
  - Vector index health

Output (text):
  OK — no issues found
  OR
  WARN  [CODE]  subject  message
  CRIT  [CODE]  subject  message

Flags:
  --json             Output as JSON
  --fail-on-warning  Exit 2 if any warning-level issues exist

Exit codes:
  0  no issues (or warnings only, unless --fail-on-warning)
  1  critical issues found
  2  warnings found (only with --fail-on-warning)

Examples:
  hermem ops integrity
  hermem ops integrity --fail-on-warning
  hermem ops integrity --json | jq '.issues | length'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ic := admin.NewIntegrityChecker(env.DB)
			report, err := ic.Check(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			if len(report.Issues) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "OK — no issues found")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, iss := range report.Issues {
				icon := "INFO"
				switch iss.Level {
				case admin.IssueCritical:
					icon = "CRIT"
				case admin.IssueWarning:
					icon = "WARN"
				}
				fmt.Fprintf(w, "%s\t[%s]\t%s\t%s\n", icon, iss.Code, iss.Subject, iss.Message)
			}
			w.Flush()

			if report.CriticalExist() {
				os.Exit(1) //nolint:revive
			}
			if failOnWarning && len(report.Issues) > 0 {
				os.Exit(2) //nolint:revive
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&failOnWarning, "fail-on-warning", false, "exit 2 if any warning-level issues exist")
	return cmd
}
