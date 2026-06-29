package helpers

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// BinaryPath returns the path to the hermem binary.
// If not found, it builds it from source.
func BinaryPath(t *testing.T) string {
	t.Helper()

	// Try common locations relative to the test package directory
	// tests/e2e/cli/ → ../../../hermem  (project root)
	// tests/e2e/http/ → ../../../hermem (project root)
	candidates := []string{
		"../../../hermem",
		"../../hermem",
		"../hermem",
		"hermem",
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "hermem"))
	}

	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	// Not found — build from source
	t.Log("hermem binary not found, building from source...")
	binPath := filepath.Join(projectRoot(t), "hermem")
	cmd := exec.Command("go", "build", "-o", binPath, "./src")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build hermem binary: %v\n%s", err, out)
	}
	return binPath
}

// projectRoot walks up from the test directory to find the Go module root.
func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// CLI wraps hermem CLI commands for testing.
type CLI struct {
	Binary  string
	WorkDir string
	Env     []string
}

// NewCLI creates a CLI wrapper.
func NewCLI(binary, workDir string) *CLI {
	return &CLI{
		Binary:  binary,
		WorkDir: workDir,
		Env:     append(os.Environ(), "HOME="+workDir),
	}
}

// Result holds the output of a CLI command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes a hermem CLI command with optional stdin.
func (c *CLI) Run(t *testing.T, args ...string) Result {
	t.Helper()
	return c.RunWithStdin(t, "", args...)
}

// RunWithStdin executes a hermem CLI command with stdin content.
func (c *CLI) RunWithStdin(t *testing.T, stdin string, args ...string) Result {
	t.Helper()
	cmd := exec.Command(c.Binary, args...)
	cmd.Dir = c.WorkDir
	cmd.Env = c.Env

	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run command: %v", err)
		}
	}

	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// MustSucceed asserts the command succeeded (exit code 0).
func (r Result) MustSucceed(t *testing.T) Result {
	t.Helper()
	if r.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout: %s\nstderr: %s",
			r.ExitCode, r.Stdout, r.Stderr)
	}
	return r
}

// MustFail asserts the command failed (exit code != 0).
func (r Result) MustFail(t *testing.T) Result {
	t.Helper()
	if r.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code, got 0\nstdout: %s", r.Stdout)
	}
	return r
}
