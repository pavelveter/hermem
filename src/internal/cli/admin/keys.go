package admin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
)

func newKeysCmd(env *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys (list, add, rotate, revoke)",
	}
	cmd.AddCommand(
		newKeysListCmd(env),
		newKeysAddCmd(env),
		newKeysRotateCmd(env),
		newKeysRevokeCmd(env),
	)
	return cmd
}

func newKeysListCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all API keys (values masked)",
		Long: `List all configured API keys with their values masked.

No input required. Reads keys directly from the hermem.ini config file.

Output (text, one key per line):
  LEGACY       admin     xxxx...xxxx
  my-key       read      yyyy...yyyy  my-key

Key values are masked (first 4 + last 4 characters shown).

Examples:
  hermem admin keys list
  hermem admin keys list | wc -l`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := env.Cfg
			if cfg.APIKey != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-10s %s\n", "LEGACY", "admin", MaskKey(cfg.APIKey))
			}
			for _, k := range cfg.APIKeys {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-10s %s  %s\n", k.Label, string(k.Scope), MaskKey(k.Value), k.Label)
			}
			if cfg.APIKey == "" && len(cfg.APIKeys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No API keys configured.")
			}
			return nil
		},
	}
}

func newKeysAddCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Generate a new API key and write to config",
		Long: `Generate a new random API key and append it to the hermem.ini config.

The command prompts interactively for:
  - Scope: read, write, or admin (default: admin)
  - Label: a human-readable name for the key

The generated key is a 64-character hex string (32 random bytes).

Output:
  Added key: <key> (scope=<scope>, label=<label>)

The key is written to [api_keys] section in hermem.ini.

Examples:
  hermem admin keys add
  hermem admin keys add   # then type: read, my-read-key`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, err := GenerateKey()
			if err != nil {
				return fmt.Errorf("generate key: %w", err)
			}
			scope, sErr := readLine(cmd, "Scope (read/write/admin) [admin]: ")
			if sErr != nil {
				return fmt.Errorf("read scope: %w", sErr)
			}
			if scope == "" {
				scope = "admin"
			}
			label, lErr := readLine(cmd, "Label: ")
			if lErr != nil {
				return fmt.Errorf("read label: %w", lErr)
			}
			path := resolveConfigPath()
			if err := config.AddKeyToFile(path, key, scope, label); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added key: %s (scope=%s, label=%s)\n", key, scope, label)
			return nil
		},
	}
}

func newKeysRotateCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <label>",
		Short: "Generate a new key for the given label",
		Long: `Rotate an API key by generating a new value for the given label.

Arguments:
  <label>   The label of the key to rotate (required)

The old key value is replaced in hermem.ini. The label and scope
are preserved. A new 64-character hex key is generated.

Output:
  Rotated key for <label>: <new-key>

⚠ Rotate keys during security incidents or on a regular schedule.
All clients using the old key will need to be updated.

Examples:
  hermem admin keys rotate my-read-key
  hermem admin keys rotate production-key`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]
			newKey, err := GenerateKey()
			if err != nil {
				return fmt.Errorf("generate key: %w", err)
			}
			path := resolveConfigPath()
			if err := config.RotateKeyInFile(path, label, newKey); err != nil {
				return fmt.Errorf("rotate key: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rotated key for %s: %s\n", label, newKey)
			return nil
		},
	}
}

func newKeysRevokeCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <label>",
		Short: "Remove a key by label or value prefix",
		Long: `Remove an API key from the hermem.ini config.

Arguments:
  <label>   The label or value prefix of the key to revoke (required)

The key entry is removed from the [api_keys] section in hermem.ini.
Clients using this key will immediately lose access.

Output:
  Revoked key: <label>

⚠ Revoking a key is irreversible. Generate a new key if needed.

Examples:
  hermem admin keys revoke my-read-key
  hermem admin keys revoke production-key`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]
			path := resolveConfigPath()
			if err := config.RemoveKeyFromFile(path, label); err != nil {
				return fmt.Errorf("revoke key: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked key: %s\n", label)
			return nil
		},
	}
}

func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func MaskKey(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func readLine(cmd *cobra.Command, prompt string) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	var line string
	_, err := fmt.Scanln(&line)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func resolveConfigPath() string {
	return config.DefaultConfigPath()
}
