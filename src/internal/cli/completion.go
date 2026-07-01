package cli

import (
	"os"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newCompletionCmd(_ *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for hermem.

To load completions:

Bash:
  $ source <(hermem completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ hermem completion bash > /etc/bash_completion.d/hermem
  # macOS:
  $ hermem completion bash > $(brew --prefix)/etc/bash_completion.d/hermem

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  # To load completions for each session, execute once:
  $ hermem completion zsh > "${fpath[1]}/_hermem"
  # You will need to start a new shell for this setup to take effect.

Fish:
  $ hermem completion fish | source
  # To load completions for each session, execute once:
  $ hermem completion fish > ~/.config/fish/completions/hermem.fish
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		PersistentPreRunE:     noopPreRun,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			}
			return nil
		},
	}
	return cmd
}
