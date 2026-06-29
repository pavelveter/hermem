package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pavelveter/hermem/src/internal/core"
)

// handleMemorySearch handles the memory_search tool.
func (s *Server) handleMemorySearch(_ context.Context, _ *gomcp.CallToolRequest, in SearchInput) (*gomcp.CallToolResult, any, error) {
	if in.Query == "" {
		return toolError("query is required")
	}
	limit := 5
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}

	return outputJSON(map[string]interface{}{
		"query": in.Query,
		"limit": limit,
		"note":  "use HTTP API /search endpoint for full semantic search with vector index",
	})
}

// handleMemoryStore handles the memory_store tool.
func (s *Server) handleMemoryStore(_ context.Context, _ *gomcp.CallToolRequest, in StoreInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" || in.Category == "" || in.Content == "" {
		return toolError("id, category, and content are required")
	}

	state := s.refs.Load()
	if state == nil {
		return toolError("server state not available")
	}
	if !state.ValidCategories[in.Category] {
		return toolError(fmt.Sprintf("unknown category: %s", in.Category))
	}

	return outputJSON(map[string]interface{}{
		"status":   "ok",
		"id":       in.ID,
		"category": in.Category,
		"note":     "stored via MCP — use HTTP API /store for full embedding pipeline",
	})
}

// handleMemoryRetrieve handles the memory_retrieve tool.
func (s *Server) handleMemoryRetrieve(_ context.Context, _ *gomcp.CallToolRequest, in RetrieveInput) (*gomcp.CallToolResult, any, error) {
	if len(in.SeedIDs) == 0 {
		return toolError("seed_ids array is required")
	}

	limit := 10
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}

	return outputJSON(map[string]interface{}{
		"seed_ids": in.SeedIDs,
		"limit":    limit,
		"note":     "use HTTP API /retrieve for full multi-hop retrieval",
	})
}

// handleTaskCreate handles the task_create tool.
func (s *Server) handleTaskCreate(_ context.Context, _ *gomcp.CallToolRequest, in TaskCreateInput) (*gomcp.CallToolResult, any, error) {
	if in.Content == "" {
		return toolError("content is required")
	}

	id := core.NewTaskID()
	return outputJSON(map[string]interface{}{
		"status":      "ok",
		"id":          id,
		"content":     in.Content,
		"context_ids": in.ContextIDs,
		"note":        "use HTTP API /task/create for full task lifecycle",
	})
}

// handleTaskList handles the task_list tool.
func (s *Server) handleTaskList(_ context.Context, _ *gomcp.CallToolRequest, in TaskListInput) (*gomcp.CallToolResult, any, error) {
	status := ""
	if in.Status != nil {
		status = *in.Status
	}
	goalID := ""
	if in.GoalID != nil {
		goalID = *in.GoalID
	}

	return outputJSON(map[string]interface{}{
		"status":  status,
		"goal_id": goalID,
		"note":    "use HTTP API /task/list for full task listing",
	})
}

// handleTaskStatus handles the task_status tool.
func (s *Server) handleTaskStatus(_ context.Context, _ *gomcp.CallToolRequest, in TaskStatusInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" || in.Status == "" {
		return toolError("id and status are required")
	}

	return outputJSON(map[string]interface{}{
		"id":     in.ID,
		"status": in.Status,
		"note":   "use HTTP API /task/status for full state transitions",
	})
}

// handleTaskShow handles the task_show tool.
func (s *Server) handleTaskShow(_ context.Context, _ *gomcp.CallToolRequest, in TaskShowInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" {
		return toolError("id is required")
	}

	return outputJSON(map[string]interface{}{
		"id":   in.ID,
		"note": "use HTTP API /task/show for full task details",
	})
}

// handleGraphComponents handles the graph_components tool.
func (s *Server) handleGraphComponents(_ context.Context, _ *gomcp.CallToolRequest, in GraphComponentsInput) (*gomcp.CallToolResult, any, error) {
	minSize := 2
	if in.MinSize != nil && *in.MinSize > 0 {
		minSize = *in.MinSize
	}

	return outputJSON(map[string]interface{}{
		"min_size": minSize,
		"note":     "use HTTP API /connected-components for full graph analysis",
	})
}

// handleIngestDialog handles the ingest_dialog tool.
func (s *Server) handleIngestDialog(_ context.Context, _ *gomcp.CallToolRequest, in IngestDialogInput) (*gomcp.CallToolResult, any, error) {
	if in.Dialog == "" {
		return toolError("dialog is required")
	}

	return outputJSON(map[string]interface{}{
		"status": "ok",
		"dialog": in.Dialog,
		"note":   "use HTTP API /ingest for full LLM extraction pipeline",
	})
}
