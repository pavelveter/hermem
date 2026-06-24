package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

// newVersionCmd prints the build metadata injected via -ldflags. Replaces
// the absence of a pre-cobra version command so users can verify they're
// on the binary they expect without scraping the help banner.
func newVersionCmd(env clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version (Version / BuildDate / GitCommit)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			b := env.Build
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n  build: %s\n  commit: %s\n",
				b.Version, b.BuildDate, b.GitCommit)
			return nil
		},
	}
}
