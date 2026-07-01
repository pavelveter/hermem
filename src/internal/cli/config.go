package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newConfigCmd(env *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management (validate / show)",
		Long: `Manage hermem configuration: validate an INI file or show the
effective configuration with defaults.

Use "hermem config <sub> --help" for details on each operation.`,
	}
	cmd.AddCommand(newConfigValidateCmd(env))
	cmd.AddCommand(newConfigShowCmd(env))
	return cmd
}

func newConfigValidateCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate hermem.ini configuration file",
		Long: `Validate the hermem.ini configuration file at the given path.

Exits 0 if valid, exits 1 with a structured error if invalid.
Default path: hermem.ini next to the binary.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := env.Cfg.Validate(); err != nil {
				return fmt.Errorf("config invalid: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ Configuration is valid")
			return nil
		},
	}
}

func newConfigShowCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show effective configuration with defaults",
		Long: `Print the effective configuration in INI format, showing all
values including defaults. Useful for debugging config issues.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := env.Cfg
			fmt.Fprintf(cmd.OutOrStdout(), `# hermem configuration (effective)
# Source: hermem.ini next to binary (or HERMEM_INI / --config flag)

[embedder]
provider = %s
url = %s
model = %s

[database]
path = %s
backend = %s

[server]
api_key = %s

[vector]
dim = %d
backend = %s

[retrieval]
max_depth = %d
max_nodes = %d
dedup_threshold = %.2f

[reranker]
provider = %s
`,
				cfg.Provider, cfg.URL, cfg.Model,
				cfg.DBPath, cfg.VectorBackend,
				maskString(cfg.APIKey),
				cfg.VectorDim, cfg.VectorBackend,
				cfg.MaxDepthCeiling, cfg.MaxRetrievedNodes, cfg.DedupThreshold,
				cfg.RerankerProvider,
			)
			return nil
		},
	}
}

func maskString(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
