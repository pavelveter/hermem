package cli

import (
	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	contradictiondomain "github.com/pavelveter/hermem/src/internal/contradiction"
	graphdomain "github.com/pavelveter/hermem/src/internal/graph"
	ingestdomain "github.com/pavelveter/hermem/src/internal/ingest"
	mcpserver "github.com/pavelveter/hermem/src/internal/mcp"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
	timelinedomain "github.com/pavelveter/hermem/src/internal/timeline"
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
  memory_search       — Search memories by semantic similarity
  memory_store        — Store a new memory
  memory_retrieve     — Retrieve contextual memories
  memory_explain      — ScoreBreakdown for a single node
  task_create         — Create a new task
  task_list           — List tasks
  task_status         — Transition task status
  task_show           — Show task details
  task_rollback       — Cascade-rollback a failed task
  task_tree           — Render task dependency tree
  graph_components    — Find connected components
  graph_communities   — Louvain community detection
  graph_contradictions — List contradiction edges
  graph_verify        — Run graph integrity checks
  ingest_dialog       — Ingest a conversation dialog

Read-only resources at hermem:// URIs:
  hermem://graph/verify        — Graph integrity report
  hermem://tasks/active        — Currently-executable tasks
  hermem://timeline/recent      — Most recent entities
  hermem://contradictions/all  — All contradiction edges

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

	// Construct domain services (same pattern as wireAll).
	memSvc := memdomain.New(env.DB, env.VI, env.Embedder)
	retSvc := retdomain.New(env.DB, env.VI, env.Embedder)
	env.Retriever = retSvc
	taskSvc := taskdomain.New(env.DB, env.Embedder, env.VI)
	graphSvc := graphdomain.New(env.DB)
	ingestSvc := ingestdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)

	srv := mcpserver.NewServer(mcpserver.Deps{
		Memory:         memSvc,
		Retrieve:       retSvc,
		Task:           taskSvc,
		Graph:          graphSvc,
		Ingest:         ingestSvc,
		Contradictions: contradictiondomain.New(env.DB),
		Timeline:       timelinedomain.New(env.DB),
		Refs:           refs,
		VectorDim:      env.Cfg.VectorDim,
	})
	return srv.Run(env.Ctx)
}
