// Package cli wires the cobra command tree for hermem — knowledge graph
// server + CLI. The runtime context (DB, vector index, embedder, etc.) is
// captured once in main.go and passed via Env; subcommands close over it
// in RunE so commands stay synchronous and there are no globals.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/cli/admin"
	"github.com/pavelveter/hermem/src/internal/cli/adminops"
	"github.com/pavelveter/hermem/src/internal/bench"
	"github.com/pavelveter/hermem/src/internal/cli/agent"
	"github.com/pavelveter/hermem/src/internal/cli/db"
	"github.com/pavelveter/hermem/src/internal/cli/diagnose"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/cli/graph"
	"github.com/pavelveter/hermem/src/internal/cli/memory"
	"github.com/pavelveter/hermem/src/internal/cli/profile"
	"github.com/pavelveter/hermem/src/internal/cli/task"
	mytime "github.com/pavelveter/hermem/src/internal/cli/time"
)

// noopPreRun is the PersistentPreRunE set on subcommands that must run
// WITHOUT database access (currently `version` and `metrics`). It is a
// package-local var (not nil) because cobra falls back to the parent's
// PersistentPreRunE when a subcommand assigns nil — and the parent's
// PersistentPreRunE opens the database, which we explicitly don't want.
var noopPreRun = func(_ *cobra.Command, _ []string) error { return nil }

const longHelp = `hermem houses a knowledge graph + vector store with an LLM-driven
extraction engine.

Group layout:
  serve|health|metrics|version    top-level server ops
  memory     store / search / retrieve / query / response / edge / ingest /
             explain / re-embed / quantize
  task       status / list / show / dep / tree / create / rollback / next
  graph      plan / recovery-plan / components / communities /
             verify / contradictions / provenance
  time       temporal / timeline
  agent      loop
  db         migrate / rollback / verify / schema
  profile    cpu / heap / goroutine / trace
  diagnose   self-check of database and memory subsystem
  (group --help shows only its own subcommands.)

All commands that take a JSON payload read it from stdin. Optional flags
use Go-style --name syntax (cobra-native). "hermem <group> --help" prints
group usage; "hermem --help" prints the full tree.`

// NewRootCommand returns the fully-wired root command. Subcommands attach
// here so main.go only needs to call NewRootCommand(env).Execute().
//
// env is taken BY VALUE because cli.<group> NewCmd factories take value-
// too for uniform signatures. PersistentPreRunE / PersistentPostRunE
// capture env by reference (Go closure semantics), so mutations inside
// EnsureDB / Close propagate to the closure-captured env. main.go's
// own env copy stays nil and never participates in cleanup — that's
// why PersistentPostRunE is wired here, not left to main.go's defer.
func NewRootCommand(env *clienv.Env) *cobra.Command {
	root := &cobra.Command{
		Use:           "hermem",
		Short:         "hermem — knowledge graph server and CLI",
		Long:          longHelp,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       env.Build.Version,
		// Cobra skips PersistentPreRunE for --help, -h, and bare
		// `./hermem` (root has no Run/RunE → cobra auto-prints help).
		// For every other subcommand this transparently opens the DB,
		// runs pending migrations, builds the vector index, and starts
		// the async metrics worker — so subcommand code can assume
		// env.DB / env.VI are non-nil.
		//
		// The lambda adapter is required: env.EnsureDB has signature
		// `func() error`; cobra's hook expects
		// `func(*cobra.Command, []string) error` — Go's method-value
		// syntax binds the receiver but leaves the parameter list
		// wrong, so an explicit lambda is needed.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return env.EnsureDB() },
		// PersistentPostRunE fires after the subcommand's RunE returns
		// (success OR error path). It is the only place we can drive
		// graceful shutdown because main.go's `defer env.Close()`
		// operates on a copy of the value-passed Env that never sees
		// EnsureDB write to it. env.Close is bool-idempotent so a
		// subsequent main defer is a no-op rather than a double-close.
		//
		// env.KeepDBOpen (set true by cli_integration_test.go and any
		// other caller that wants to drive multiple commands against
		// one env) short-circuits the close so the DB stays open across
		// executeCmd boundaries. The teardown is left to t.Cleanup
		// in tests and to main.go's own defer in production.
		PersistentPostRunE: func(_ *cobra.Command, _ []string) error {
			if !env.KeepDBOpen {
				env.Close()
			}
			return nil
		},
	}
	root.AddCommand(
		newServeCmd(env),
		newHealthCmd(env),
		newMetricsCmd(env),
		newVersionCmd(env),
		admin.NewCmd(env),
		memory.NewCmd(env),
		task.NewCmd(env),
		graph.NewCmd(env),
		mytime.NewCmd(env),
		agent.NewCmd(env),
		db.NewCmd(env),
		profile.NewCmd(env),
		bench.NewCmd(env),
		diagnose.NewCmd(env),
	)
	adminops.Register(root, env)
	root.SetContext(env.Ctx)
	return root
}
