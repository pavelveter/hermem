// idcmd implements ADR-035 decision 4 tooling:
//
//	hermem id inspect <id>...   # validate grammar + decode timestamp
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/id"
)

func newIDCmd(_ *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "id",
		Short: "Identifier utilities (ADR-035)",
		Long: `Validate and inspect server-minted identifiers.

Grammar: <type>-<26 Crockford base32 chars>, types: task | ent | ep | job.
Task IDs embed their creation millisecond (ULID); entity IDs are
content-addressed and carry no clock.`,
	}
	inspect := &cobra.Command{
		Use:   "inspect <id>...",
		Short: "Validate IDs and decode their embedded metadata",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			exit := 0
			for _, raw := range args {
				info, err := id.Inspect(raw)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s\tINVALID (%v)\n", raw, err)
					exit = 1
					continue
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%s\tkind=%s", raw, info.Kind)
				if info.Time != "" {
					fmt.Fprintf(&b, "\tcreated=%s", info.Time)
				}
				fmt.Fprintln(cmd.OutOrStdout(), b.String())
			}
			if exit != 0 {
				return errors.New("one or more identifiers failed validation")
			}
			return nil
		},
		PersistentPreRunE: noopPreRun,
	}
	cmd.AddCommand(inspect)
	return cmd
}
