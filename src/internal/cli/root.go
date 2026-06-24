// Package cli wires the cobra command tree for hermem — knowledge graph
// server + CLI. The runtime context (DB, vector index, embedder, etc.) is
// captured once in main.go and passed via Env; subcommands close over it
// in RunE so commands stay synchronous and there are no globals.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/cli/agent"
	"github.com/pavelveter/hermem/src/internal/cli/db"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/cli/graph"
	"github.com/pavelveter/hermem/src/internal/cli/memory"
	"github.com/pavelveter/hermem/src/internal/cli/task"
	mytime "github.com/pavelveter/hermem/src/internal/cli/time"
)

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
  (group --help shows only its own subcommands.)

All commands that take a JSON payload read it from stdin. Optional flags
use Go-style --name syntax (cobra-native). "hermem <group> --help" prints
group usage; "hermem --help" prints the full tree.`

// NewRootCommand returns the fully-wired root command. Subcommands attach
// here so main.go only needs to call NewRootCommand(env).Execute().
func NewRootCommand(env clienv.Env) *cobra.Command {
	root := &cobra.Command{
		Use:           "hermem",
		Short:         "hermem — knowledge graph server and CLI",
		Long:          longHelp,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       env.Build.Version,
	}
	root.AddCommand(
		newServeCmd(env),
		newHealthCmd(env),
		newMetricsCmd(env),
		newVersionCmd(env),
		memory.NewCmd(env),
		task.NewCmd(env),
		graph.NewCmd(env),
		mytime.NewCmd(env),
		agent.NewCmd(env),
		db.NewCmd(env),
	)
	return root
}
