package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newCompletionCmd(_ *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts (bash, zsh, fish)",
		Long: `Generate shell completion scripts for hermem.

The argument must be one of: bash, zsh, or fish.

Examples:
  hermem completion bash        # Bash completions to stdout
  hermem completion zsh         # Zsh completions to stdout
  hermem completion fish        # Fish completions to stdout

To load completions:

Bash (one-time):
  source <(hermem completion bash)
  # Or install system-wide:
  hermem completion bash > /etc/bash_completion.d/hermem       # Linux
  hermem completion bash > $(brew --prefix)/etc/bash_completion.d/hermem  # macOS

Zsh (one-time):
  # Enable compinit in ~/.zshrc if not already:
  echo "autoload -U compinit; compinit" >> ~/.zshrc
  # Then install completions:
  hermem completion zsh > "${fpath[1]}/_hermem"
  # Start a new shell to activate.

Fish (one-time):
  hermem completion fish > ~/.config/fish/completions/hermem.fish`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		PersistentPreRunE:     noopPreRun,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Write to cmd.OutOrStdout() rather than os.Stdout so the
			// generator honours the cobra output redirection set via
			// cmd.SetOut — required for unit tests to capture output
			// without spawning a subprocess.
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(out)
			case "fish":
				return cmd.Root().GenFishCompletion(out, true)
			}
			return nil
		},
	}
	return cmd
}
