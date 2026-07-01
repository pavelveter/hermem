package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newDocsCmd(_ *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs [directory]",
		Short: "Generate CLI documentation in Markdown format",
		Long: `Generate comprehensive CLI documentation for all hermem commands.

Creates one Markdown file per command in the specified directory (default: docs/cli/).
Each file contains the command's description, usage, flags, and examples.`,
		Args:                  cobra.MaximumNArgs(1),
		DisableFlagsInUseLine: true,
		PersistentPreRunE:     noopPreRun,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "docs/cli"
			if len(args) > 0 {
				dir = args[0]
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create docs directory: %w", err)
			}

			root := cmd.Root()
			header := &doc.GenManHeader{
				Title:   "HERMEM",
				Section: "1",
			}

			prepender := func(filename string) string {
				name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
				name = strings.ReplaceAll(name, "_", " ")
				return fmt.Sprintf("---\ntitle: %s\n---\n\n", name)
			}

			linkHandler := func(name string) string {
				return name + ".md"
			}

			// Generate Markdown files
			if err := doc.GenMarkdownTreeCustom(root, dir, prepender, linkHandler); err != nil {
				return fmt.Errorf("generate markdown docs: %w", err)
			}

			// Generate man pages
			manDir := filepath.Join(dir, "man")
			if err := os.MkdirAll(manDir, 0o755); err != nil {
				return fmt.Errorf("create man directory: %w", err)
			}
			if err := doc.GenManTree(root, header, manDir); err != nil {
				return fmt.Errorf("generate man pages: %w", err)
			}

			// Count generated files
			var mdCount, manCount int
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if strings.HasSuffix(path, ".md") {
					mdCount++
				}
				if strings.HasSuffix(path, ".1") {
					manCount++
				}
				return nil
			})

			fmt.Fprintf(os.Stderr, "Generated %d Markdown files and %d man pages in %s\n", mdCount, manCount, dir)
			return nil
		},
	}
	return cmd
}
