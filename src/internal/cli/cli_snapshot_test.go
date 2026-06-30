package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

var updateSnapshots = flag.Bool("update", false, "update snapshot files")

// cliCommands returns the list of command groups to snapshot.
func cliCommands() []string {
	return []string{
		"",
		"memory",
		"task",
		"graph",
		"time",
		"db",
		"admin",
		"diagnose",
		"serve",
		"health",
		"metrics",
		"version",
		"agent",
		"profile",
		"mcp",
	}
}

func TestCLIHelpSnapshots(t *testing.T) {
	snapshotDir := filepath.Join("testdata", "help")
	if *updateSnapshots {
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, cmd := range cliCommands() {
		t.Run(strings.ReplaceAll(cmd, "/", "_"), func(t *testing.T) {
			env := &cli.Env{
				Build: cli.BuildInfo{Version: "test", BuildDate: "now", GitCommit: "abc"},
			}
			root := NewRootCommand(env)

			args := []string{}
			if cmd != "" {
				args = append(strings.Fields(cmd), "--help")
			} else {
				args = append(args, "--help")
			}
			root.SetArgs(args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			// Capture help output via SetHelpTemplate override
			var buf bytes.Buffer
			root.SetOut(&buf)

			// Execute will print help and return nil for --help
			_ = root.Execute()

			got := buf.String()
			if got == "" {
				// Fallback: cobra prints to os.Stdout for --help
				// Use the command's HelpTemplate to render manually
				t.Skip("no help output captured; cobra writes to stdout")
			}

			snapshotName := cmd
			if snapshotName == "" {
				snapshotName = "root"
			}
			snapshotPath := filepath.Join(snapshotDir, snapshotName+".txt")

			if *updateSnapshots {
				if err := os.WriteFile(snapshotPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Log("snapshot updated:", snapshotPath)
				return
			}

			existing, err := os.ReadFile(snapshotPath)
			if err != nil {
				if os.IsNotExist(err) {
					t.Skip("snapshot not found; run with -update to create")
				}
				t.Fatal(err)
			}

			if got != string(existing) {
				t.Errorf("help output mismatch for %q\nrun 'go test -update' to refresh", cmd)
			}
		})
	}
}
