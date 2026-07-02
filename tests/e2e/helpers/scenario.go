package helpers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Scenario represents a YAML-driven test scenario.
type Scenario struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

// Step represents a single step in a scenario.
type Step struct {
	Name     string    `yaml:"name"`
	CLI      *CLIStep  `yaml:"cli,omitempty"`
	HTTP     *HTTPStep `yaml:"http,omitempty"`
	Expected *Expected `yaml:"expected,omitempty"`
}

// CLIStep represents a CLI command to execute.
type CLIStep struct {
	Args  []string `yaml:"args"`
	Stdin string   `yaml:"stdin,omitempty"`
}

// HTTPStep represents an HTTP request to execute.
type HTTPStep struct {
	Method string      `yaml:"method"`
	Path   string      `yaml:"path"`
	Body   interface{} `yaml:"body,omitempty"`
}

// Expected represents expected results after a step.
type Expected struct {
	ExitCode *int                   `yaml:"exit_code,omitempty"`
	Status   *int                   `yaml:"status,omitempty"`
	Fields   map[string]interface{} `yaml:"fields,omitempty"`
}

// RunScenario executes a YAML scenario through both CLI and HTTP interfaces.
func RunScenario(t *testing.T, dir string, scenarioFile string) {
	t.Helper()

	data, err := os.ReadFile(scenarioFile)
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}

	var scenario Scenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		t.Fatalf("parse scenario: %v", err)
	}

	t.Run(scenario.Name, func(t *testing.T) {
		SkipIfNoEmbedder(t)
		cli := NewCLI(BinaryPath(t), dir)
		var client *HTTPClient

		for i, step := range scenario.Steps {
			t.Run(step.Name, func(t *testing.T) {
				// Start server lazily
				if client == nil && step.HTTP != nil {
					srv := StartServer(t, dir)
					client = NewHTTPClient(srv.URL)
				}

				if step.CLI != nil {
					runCLIStep(t, cli, step)
				}
				if step.HTTP != nil {
					runHTTPStep(t, client, step)
				}

				_ = i // step index available for debugging
			})
		}
	})
}

func runCLIStep(t *testing.T, cli *CLI, step Step) {
	t.Helper()
	if step.CLI == nil {
		return
	}

	result := cli.RunWithStdin(t, step.CLI.Stdin, step.CLI.Args...)

	if step.Expected != nil && step.Expected.ExitCode != nil {
		if result.ExitCode != *step.Expected.ExitCode {
			t.Fatalf("expected exit code %d, got %d\nstdout: %s\nstderr: %s",
				*step.Expected.ExitCode, result.ExitCode, result.Stdout, result.Stderr)
		}
	}
}

func runHTTPStep(t *testing.T, client *HTTPClient, step Step) {
	t.Helper()
	if step.HTTP == nil || client == nil {
		return
	}

	var resp *http.Response
	switch strings.ToUpper(step.HTTP.Method) {
	case "GET":
		resp = client.Get(t, step.HTTP.Path)
	case "POST":
		resp = client.Post(t, step.HTTP.Path, step.HTTP.Body)
	default:
		t.Fatalf("unsupported HTTP method: %s", step.HTTP.Method)
	}

	if step.Expected != nil && step.Expected.Status != nil {
		MustStatus(t, resp, *step.Expected.Status)
	}

	if step.Expected != nil && step.Expected.Fields != nil {
		m := MustJSONMap(t, resp)
		for path, expected := range step.Expected.Fields {
			AssertJSONField(t, m, path, expected)
		}
	}
}

// RunAllScenarios runs all YAML scenarios in the given directory.
//
// Each scenario YAML gets its OWN per-scenario workspace (fresh
// hermem.ini copy + fresh hermem.db). The previous implementation
// shared `dir` across every scenario, which caused the
// `schema_migrations` UNIQUE-constraint race + duplicate-column
// errors observed by `TestAllScenarios`. The parent `dir` is now
// only used as a config template — its hermem.ini content is
// copied into each per-scenario workspace before RunScenario runs.
func RunAllScenarios(t *testing.T, dir string, scenariosDir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(scenariosDir, "*.yaml"))
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}

	parentCfg, err := os.ReadFile(filepath.Join(dir, "hermem.ini"))
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}

	for _, f := range files {
		scenDir, _ := TempWorkspace(t)
		scenIni := filepath.Join(scenDir, "hermem.ini")
		if err := os.WriteFile(scenIni, parentCfg, 0644); err != nil {
			t.Fatalf("copy config to scenario dir: %v", err)
		}
		RunScenario(t, scenDir, f)
	}
}
