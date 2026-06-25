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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
