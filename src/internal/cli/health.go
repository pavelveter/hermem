package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

// newHealthCmd pings the DB and prints a JSON status. Exit 1 on failure
// (cobra turns the RunE error into exit 1 in main.go). Mirrors the
// GET /health/ready HTTP handler so a CLI-only deployment can probe the
// process without an HTTP roundtrip.
func newHealthCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Health probe (pings the DB; mirrors /health/ready)",
		Long: `Run a health check against the local database.

Pings the SQLite connection and returns a JSON status object.
Mirrors the GET /health/ready HTTP endpoint. Useful for monitoring
scripts and container liveness probes that don't want an HTTP roundtrip.

Exit code 0 = healthy, 1 = unhealthy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := env.DB.PingContext(env.Ctx); err != nil {
				fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
				return fmt.Errorf("unhealthy: %w", err)
			}
			return clienv.WriteJSON(cmd.OutOrStdout(), map[string]any{
				"status": "ok",
				"checks": map[string]string{"database": "ok"},
			})
		},
	}
}
