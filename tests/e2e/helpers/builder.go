package helpers

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Builder provides a fluent DSL for composing e2e test scenarios.
// Usage:
//
//	helpers.NewBuilder(t).
//	    Store("e1", "world", "Paris is the capital of France").
//	    Search("capital of France").
//	    ExpectResults(1).
//	    Run()
type Builder struct {
	t      *testing.T
	dir    string
	cli    *CLI
	client *HTTPClient
	steps  []step
}

type step struct {
	name string
	fn   func(t *testing.T)
}

// NewBuilder creates a new test builder for the given workspace directory.
func NewBuilder(t *testing.T) *Builder {
	t.Helper()
	dir, _ := TempWorkspace(t)
	WriteConfigForCLI(t, dir, DefaultConfig(DBPath(dir)))
	return &Builder{
		t:   t,
		dir: dir,
		cli: NewCLI(BinaryPath(t), dir),
	}
}

// Dir returns the workspace directory for additional setup.
func (b *Builder) Dir() string { return b.dir }

// CLI returns the underlying CLI helper for custom commands.
func (b *Builder) CLI() *CLI { return b.cli }

// Store adds a CLI memory store step.
func (b *Builder) Store(id, category, content string) *Builder {
	b.steps = append(b.steps, step{
		name: fmt.Sprintf("store(%s)", id),
		fn: func(t *testing.T) {
			t.Helper()
			body := fmt.Sprintf(`{"id":"%s","category":"%s","content":"%s"}`, id, category, content)
			b.cli.RunWithStdin(t, body, "memory", "store").MustSucceed(t)
		},
	})
	return b
}

// StoreRaw adds a CLI memory store step with raw JSON input.
func (b *Builder) StoreRaw(input string) *Builder {
	b.steps = append(b.steps, step{
		name: "store(raw)",
		fn: func(t *testing.T) {
			t.Helper()
			b.cli.RunWithStdin(t, input, "memory", "store").MustSucceed(t)
		},
	})
	return b
}

// Search adds a CLI memory search step.
func (b *Builder) Search(query string) *Builder {
	b.steps = append(b.steps, step{
		name: fmt.Sprintf("search(%s)", query),
		fn: func(t *testing.T) {
			t.Helper()
			body := fmt.Sprintf(`{"query":"%s","top_k":5}`, query)
			b.cli.RunWithStdin(t, body, "memory", "search").MustSucceed(t)
		},
	})
	return b
}

// CLICmd adds a custom CLI command step.
func (b *Builder) CLICmd(args ...string) *Builder {
	b.steps = append(b.steps, step{
		name: fmt.Sprintf("cli(%v)", args),
		fn: func(t *testing.T) {
			t.Helper()
			b.cli.RunWithStdin(t, "", args...).MustSucceed(t)
		},
	})
	return b
}

// ExpectResults adds an assertion that the last search returned results.
func (b *Builder) ExpectResults(minCount int) *Builder {
	b.steps = append(b.steps, step{
		name: fmt.Sprintf("expect_results(>=%d)", minCount),
		fn: func(t *testing.T) {
			t.Helper()
			body := fmt.Sprintf(`{"query":"test","top_k":5}`)
			result := b.cli.RunWithStdin(t, body, "memory", "search")
			result.MustSucceed(t)
			var parsed struct {
				Results []json.RawMessage `json:"results"`
			}
			if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
				t.Fatalf("parse search results: %v", err)
			}
			if len(parsed.Results) < minCount {
				t.Fatalf("expected >= %d results, got %d", minCount, len(parsed.Results))
			}
		},
	})
	return b
}

// Run executes all accumulated steps in order.
func (b *Builder) Run() {
	b.t.Helper()
	for _, s := range b.steps {
		b.t.Run(s.name, s.fn)
	}
}
