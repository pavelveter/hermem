// Package mcp provides the Model Context Protocol (MCP) server for Hermem.
// It exposes Hermem memory, task, and graph operations as MCP tools,
// allowing AI assistants to interact with the knowledge base directly.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	contradictiondomain "github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	graphdomain "github.com/pavelveter/hermem/src/internal/graph"
	ingestdomain "github.com/pavelveter/hermem/src/internal/ingest"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	retrievaldomain "github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
	timelinedomain "github.com/pavelveter/hermem/src/internal/timeline"
)

// Deps holds the domain services the MCP server needs.
type Deps struct {
	Memory         *memdomain.Service
	Retrieve       *retrievaldomain.Service
	Task           *taskdomain.Service
	Graph          *graphdomain.Service
	Ingest         *ingestdomain.Service
	Contradictions *contradictiondomain.Service
	Timeline       *timelinedomain.Service
	Refs           *serverstate.Ref
	VectorDim      int
}

// Server wraps the MCP server with Hermem domain services.
type Server struct {
	mcpServer *gomcp.Server
	deps      Deps
	limiter   *RateLimiter
}

// NewServer creates a new MCP server with all Hermem tools registered.
func NewServer(deps Deps) *Server {
	s := &Server{
		deps:    deps,
		limiter: NewRateLimiter(10, 2), // 10 burst, 2 req/s sustained
	}
	s.mcpServer = gomcp.NewServer(
		&gomcp.Implementation{
			Name:    "hermem",
			Version: "1.0.0",
		},
		nil,
	)
	s.registerTools()
	s.registerResources()
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

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "graph_communities",
		Description: "Detect knowledge clusters via Louvain community detection. Returns community IDs, member lists, sizes, and modularity scores.",
	}, s.handleGraphCommunities)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "graph_verify",
		Description: "Run graph integrity checks: orphaned edges, missing vector descriptors, dimension mismatches. Returns a list of issues (empty = clean).",
	}, s.handleGraphVerify)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_tree",
		Description: "Render the task dependency tree (blocked_by edges) as a human-readable ASCII tree for a given goal or globally.",
	}, s.handleTaskTree)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "task_rollback",
		Description: "Cascade-rollback a failed task and all tasks blocked-by it. Appends error context to each rolled-back task's content.",
	}, s.handleTaskRollback)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "graph_contradictions",
		Description: "List all contradiction edges in the knowledge graph, or filter by entity ID. Each pair shows two conflicting nodes with their content.",
	}, s.handleGraphContradictions)

	gomcp.AddTool(s.mcpServer, &gomcp.Tool{
		Name:        "memory_explain",
		Description: "Return a ScoreBreakdown for a single memory node by ID. Shows vector similarity, recency decay, temporal decay, and graph centrality. Pass an optional query for vector-similarity context.",
	}, s.handleMemoryExplain)
}

// Input types for tools.

type SearchInput struct {
	Query string `json:"query"`
	Limit *int   `json:"limit,omitempty"`
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

type GraphCommunitiesInput struct {
	MaxIterations *int `json:"max_iterations,omitempty"`
}

type GraphVerifyInput struct{}

type TaskTreeInput struct {
	GoalID *string `json:"goal_id,omitempty"`
}

type TaskRollbackInput struct {
	ID           string `json:"id"`
	ErrorContext string `json:"error_context,omitempty"`
}

type GraphContradictionsInput struct {
	ID *string `json:"id,omitempty"`
}

type MemoryExplainInput struct {
	ID    string  `json:"id"`
	Query *string `json:"query,omitempty"`
}

// schema returns the current schema config from server state.
func (s *Server) schema() core.SchemaConfig {
	state := s.deps.Refs.Load()
	if state == nil {
		return core.DefaultSchemaConfig(false)
	}
	return state.Schema
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

// resourceJSON marshals v to JSON and wraps it as a single ResourceContents entry.
func resourceJSON(uri string, v interface{}) (*gomcp.ReadResourceResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &gomcp.ReadResourceResult{
		Contents: []*gomcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(b),
		}},
	}, nil
}

// registerResources registers all Hermem MCP resources (read-only data URIs).
func (s *Server) registerResources() {
	s.mcpServer.AddResource(&gomcp.Resource{
		URI:         "hermem://graph/verify",
		Name:        "Graph Integrity",
		Description: "Current graph integrity status: orphaned edges, dimension mismatches. Empty issues list = clean.",
		MIMEType:    "application/json",
	}, s.handleGraphVerifyResource)

	s.mcpServer.AddResource(&gomcp.Resource{
		URI:         "hermem://tasks/active",
		Name:        "Active Tasks",
		Description: "Currently-executable tasks (all blockers done). Ready for processing.",
		MIMEType:    "application/json",
	}, s.handleTasksActiveResource)

	s.mcpServer.AddResource(&gomcp.Resource{
		URI:         "hermem://timeline/recent",
		Name:        "Recent Timeline",
		Description: "Most recently created entities (up to 100).",
		MIMEType:    "application/json",
	}, s.handleTimelineRecentResource)

	s.mcpServer.AddResource(&gomcp.Resource{
		URI:         "hermem://contradictions/all",
		Name:        "All Contradictions",
		Description: "Every contradiction edge in the knowledge graph with node content.",
		MIMEType:    "application/json",
	}, s.handleContradictionsAllResource)
}

// handleGraphVerifyResource returns the current graph integrity report.
func (s *Server) handleGraphVerifyResource(ctx context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
	state := s.deps.Refs.Load()
	schema := core.DefaultSchemaConfig(false)
	dim := s.deps.VectorDim
	if state != nil {
		schema = state.Schema
	}
	report, err := s.deps.Graph.Verify(ctx, schema, dim)
	if err != nil {
		return nil, err
	}
	return resourceJSON("hermem://graph/verify", map[string]interface{}{
		"pass":   report.Pass(),
		"issues": report.Issues,
		"count":  len(report.Issues),
	})
}

// handleTasksActiveResource returns currently-executable tasks.
func (s *Server) handleTasksActiveResource(ctx context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
	state := s.deps.Refs.Load()
	schema := core.DefaultSchemaConfig(false)
	if state != nil {
		schema = state.Schema
	}
	tasks, err := s.deps.Task.Executable(ctx, "", schema)
	if err != nil {
		return nil, err
	}
	return resourceJSON("hermem://tasks/active", map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// handleTimelineRecentResource returns the most recently created entities.
func (s *Server) handleTimelineRecentResource(ctx context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
	if s.deps.Timeline == nil {
		return nil, fmt.Errorf("timeline service not available")
	}
	entries, err := s.deps.Timeline.Timeline(ctx, 100)
	if err != nil {
		return nil, err
	}
	return resourceJSON("hermem://timeline/recent", map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	})
}

// handleContradictionsAllResource returns all contradiction edges.
func (s *Server) handleContradictionsAllResource(ctx context.Context, _ *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
	if s.deps.Contradictions == nil {
		return nil, fmt.Errorf("contradiction service not available")
	}
	pairs, err := s.deps.Contradictions.List(ctx, "")
	if err != nil {
		return nil, err
	}
	return resourceJSON("hermem://contradictions/all", map[string]interface{}{
		"pairs": pairs,
		"count": len(pairs),
	})
}
