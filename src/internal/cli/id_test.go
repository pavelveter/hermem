package cli

import (
	"strings"
	"testing"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/id"
)

// TestIDInspect_ValidAndInvalid pins the ADR-035 decision-4 surface:
// generated IDs validate and decode their timestamp; legacy counter
// IDs ("task-1") are rejected.
func TestIDInspect_ValidAndInvalid(t *testing.T) {
	valid := id.NewTaskID()
	cmd := newIDCmd(&cli.Env{})
	cmd.SetOut(nil)
	cmd.SetErr(nil)

	var out, errOut strings.Builder
	cmd.SetOutput(&out) //nolint:staticcheck // cobra pre-1.8 API used by the suite
	inspectCmd, _, err := cmd.Find([]string{"inspect"})
	if err != nil {
		t.Fatal(err)
	}
	inspectCmd.SetOut(&out)
	inspectCmd.SetErr(&errOut)

	if err := inspectCmd.RunE(inspectCmd, []string{valid}); err != nil {
		t.Fatalf("valid ID rejected: %v", err)
	}
	if !strings.Contains(out.String(), "kind=task") || !strings.Contains(out.String(), "created=") {
		t.Fatalf("output missing kind/created: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	err = inspectCmd.RunE(inspectCmd, []string{"task-1"})
	if err == nil {
		t.Fatal("legacy task-1 accepted; want validation failure")
	}
	if !strings.Contains(errOut.String(), "INVALID") {
		t.Fatalf("stderr missing INVALID marker: %q", errOut.String())
	}
}
