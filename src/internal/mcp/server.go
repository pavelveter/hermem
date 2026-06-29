// Package mcp provides the Model Context Protocol (MCP) server for Hermem.
// It exposes Hermem memory, task, and graph operations as MCP tools,
// allowing AI assistants to interact with the knowledge base.
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// Server wraps the MCP server with Hermem dependencies.
type Server struct {
	mcpServer *gomcp.Server
	refs      *serverstate.Ref
}

// NewServer creates a new MCP server with all Hermem tools registered.
func NewServer(refs *serverstate.Ref) *Server {
	s := &Server{refs: refs}
	s.mcpServer = gomcp.NewServer(
		&gomcp.Implementation{
			Name:    "hermem",
			Version: "1.0.0",
		},
		nil,
	)
	s.registerTools()
	return s
}

// Run starts the MCP server over stdio transport. Blocks until the client disconnects.
func (s *Server) Run(ctx context.Context) error {
	slog.Info("mcp server starting", "name", "hermem", "version", "1.0.0")
	return s.mcpServer.Run(ctx, &gomcp.StdioTransport{})
}

// registerTools registers all Hermem MCP tools.
func (s *Server) registerTools() {
	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "memory_search",
		Description: "Search memories by semantic similarity. Returns the most relevant stored memories matching a natural language query.",
	}, s.handleMemorySearch)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "memory_store",
		Description: "Store a new memory in the knowledge base. Creates a persistent fact, opinion, or observation.",
	}, s.handleMemoryStore)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "memory_retrieve",
		Description: "Retrieve contextual memories around given seed IDs. Returns connected memories with relevance scores.",
	}, s.handleMemoryRetrieve)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_create",
		Description: "Create a new task in the knowledge base. Tasks are stateful entities with lifecycle management.",
	}, s.handleTaskCreate)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_list",
		Description: "List tasks filtered by status and/or goal ID.",
	}, s.handleTaskList)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_status",
		Description: "Transition a task to a new status (e.g., pending -> in_progress, in_progress -> done).",
	}, s.handleTaskStatus)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_show",
		Description: "Show detailed information about a specific task, including blocked-by and recovery relationships.",
	}, s.handleTaskShow)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "graph_components",
		Description: "Find connected components in the knowledge graph. Useful for discovering clusters of related memories.",
	}, s.handleGraphComponents)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "ingest_dialog",
		Description: "Ingest a conversation dialog. Extracts entities, facts, and relationships using LLM.",
	}, s.handleIngestDialog)
}

// Input types for tools.

type SearchInput struct {
	Query string  `json:"query"`
	Limit *int    `json:"limit,omitempty"`
}

type StoreInput struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

type RetrieveInput struct {
	SeedIDs []string `json:"seed_ids"`
	Limit   *int     `json:"limit,omitempty"`
}

type TaskCreateInput struct {
	Content    string   `json:"content"`
	ContextIDs []string `json:"context_ids,omitempty"`
}

type TaskListInput struct {
	Status *string `json:"status,omitempty"`
	GoalID *string `json:"goal_id,omitempty"`
}

type TaskStatusInput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskShowInput struct {
	ID string `json:"id"`
}

type GraphComponentsInput struct {
	MinSize *int `json:"min_size,omitempty"`
}

type IngestDialogInput struct {
	Dialog string `json:"dialog"`
}

// outputJSON marshals a value to JSON and returns it as text content.
func outputJSON(v interface{}) (*gomcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{&gomcp.TextContent{
			Text: string(b),
		}},
	}, nil, nil
}

// toolError returns a tool error result.
func toolError(msg string) (*gomcp.CallToolResult, any, error) {
	return &gomcp.CallToolResult{
		IsError: true,
		Content: []gomcp.Content{&gomcp.TextContent{
			Text: msg,
		}},
	}, nil, nil
}
