package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	mcpserver "github.com/pavelveter/hermem/src/internal/mcp"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// newMCPCmd starts the MCP server over stdio for AI assistant integration.
func newMCPCmd(env *clienv.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio transport for AI assistants)",
		Long: `Start the Model Context Protocol server over stdio.

This exposes Hermem memory, task, and graph operations as MCP tools,
allowing AI assistants (Claude, GPT, etc.) to interact with the knowledge
base directly.

Tools available:
  memory_search    — Search memories by semantic similarity
  memory_store     — Store a new memory
  memory_retrieve  — Retrieve contextual memories
  task_create      — Create a new task
  task_list        — List tasks
  task_status      — Transition task status
  task_show        — Show task details
  graph_components — Find connected components
  ingest_dialog    — Ingest a conversation dialog

Usage with Claude Desktop or Claude Code:
  Add to your MCP config:
  {
    "mcpServers": {
      "hermem": {
        "command": "hermem",
        "args": ["mcp"]
      }
    }
  }`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCP(env)
		},
	}
}

func runMCP(env *clienv.Env) error {
	refs := serverstate.NewRef(buildState(env.Cfg, env.Reranker))
	srv := mcpserver.NewServer(refs)
	return srv.Run(env.Ctx)
}
