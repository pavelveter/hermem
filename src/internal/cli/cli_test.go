package cli

import (
	"testing"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// TestNewRoot_HasExpectedCommands asserts the full top-level command tree.
// Cobra's --help output is implicitly covered since each command's Use
// string is non-empty.
func TestNewRoot_HasExpectedCommands(t *testing.T) {
	cmd := NewRootCommand(&cli.Env{
		Build: cli.BuildInfo{Version: "test", BuildDate: "now", GitCommit: "abc"},
	})

	want := map[string]bool{
		"serve":   false,
		"health":  false,
		"metrics": false,
		"version": false,
		"memory":  false,
		"task":    false,
		"graph":   false,
		"time":    false,
		"agent":   false,
		"db":      false,
	}
	for _, c := range cmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("missing top-level command: %q", name)
		}
	}
}

// TestRoot_HelpRunsWithoutPanic ensures cobra can render --help without
// crashing — catches mis-formed Use/Short strings at unit-test time.
func TestRoot_HelpRunsWithoutPanic(t *testing.T) {
	cmd := NewRootCommand(&cli.Env{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("root --help errored: %v", err)
	}
}
