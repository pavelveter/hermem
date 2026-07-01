package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletionCommand verifies that the `hermem completion <shell>`
// subcommand emits non-empty output for each supported shell. The
// cobra-generated preamble is shell-specific, so we assert that the
// canonical marker for each shell appears in the output:
//
//	bash → "# bash completion"
//	zsh  → "#compdef"
//	fish → "complete -c"
//
// This is a smoke test: it does not assert every command is completed,
// only that the generator runs cleanly and produces output. Full
// completion correctness is covered by cobra's own test surface.
func TestCompletionCommand(t *testing.T) {
	rootCmd := &cobra.Command{Use: "hermem"}
	rootCmd.AddCommand(newCompletionCmd(nil))

	cases := []struct {
		shell string
		hint  string
	}{
		{"bash", "# bash completion"},
		{"zsh", "#compdef"},
		{"fish", "complete -c"},
	}
	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs([]string{"completion", tc.shell})
			err := rootCmd.Execute()
			require.NoError(t, err, "hermem completion %s should not error", tc.shell)
			output := buf.String()
			assert.NotEmpty(t, output, "hermem completion %s produced empty output", tc.shell)
			assert.Contains(t, output, tc.hint,
				"hermem completion %s missing expected marker %q", tc.shell, tc.hint)
		})
	}
}

// TestCompletionCommandInvalidShell guards against drift in ValidArgs
// or the switch-case dispatch in completion.go — adding a new shell
// without registering it would silently emit empty output.
func TestCompletionCommandInvalidShell(t *testing.T) {
	rootCmd := &cobra.Command{Use: "hermem"}
	rootCmd.AddCommand(newCompletionCmd(nil))
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"completion", "powershell"})
	// cobra.OnlyValidArgs returns an error rather than dispatching.
	err := rootCmd.Execute()
	assert.Error(t, err, "expected error for unsupported shell")
}
