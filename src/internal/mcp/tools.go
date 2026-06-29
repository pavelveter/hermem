package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pavelveter/hermem/src/internal/core"
)

// handleMemorySearch searches memories by semantic similarity via the retrieval service.
func (s *Server) handleMemorySearch(ctx context.Context, _ *gomcp.CallToolRequest, in SearchInput) (*gomcp.CallToolResult, any, error) {
	if in.Query == "" {
		return toolError("query is required")
	}
	limit := 5
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}

	results, err := s.deps.Retrieve.Search(ctx, in.Query, limit)
	if err != nil {
		return toolError(fmt.Sprintf("search failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"query":   in.Query,
		"results": results,
		"count":   len(results),
	})
}

// handleMemoryStore stores a new memory via the memory service.
func (s *Server) handleMemoryStore(ctx context.Context, _ *gomcp.CallToolRequest, in StoreInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" || in.Category == "" || in.Content == "" {
		return toolError("id, category, and content are required")
	}

	state := s.deps.Refs.Load()
	if state == nil {
		return toolError("server state not available")
	}
	if !state.ValidCategories[in.Category] {
		return toolError(fmt.Sprintf("unknown category: %s", in.Category))
	}

	err := s.deps.Memory.Store(ctx, core.StoreRequest{
		ID:       in.ID,
		Category: in.Category,
		Content:  in.Content,
	}, state.Schema)
	if err != nil {
		return toolError(fmt.Sprintf("store failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"status":   "ok",
		"id":       in.ID,
		"category": in.Category,
	})
}

// handleMemoryRetrieve retrieves contextual memories via the retrieval service.
func (s *Server) handleMemoryRetrieve(ctx context.Context, _ *gomcp.CallToolRequest, in RetrieveInput) (*gomcp.CallToolResult, any, error) {
	if len(in.SeedIDs) == 0 {
		return toolError("seed_ids array is required")
	}

	limit := 10
	if in.Limit != nil && *in.Limit > 0 {
		limit = *in.Limit
	}

	state := s.deps.Refs.Load()
	opts := core.RetrieveContextOptions{TopK: limit}
	if state != nil {
		opts.DepthCeiling = state.DepthCeiling
		opts.MaxRetrievedNodes = state.MaxRetrievedNodes
		opts.RankingWeight = state.RankingWeight
	}

	result, err := s.deps.Retrieve.Retrieve(ctx, in.SeedIDs, opts)
	if err != nil {
		return toolError(fmt.Sprintf("retrieve failed: %v", err))
	}

	return outputJSON(result)
}

// handleTaskCreate creates a new task via the task service.
func (s *Server) handleTaskCreate(ctx context.Context, _ *gomcp.CallToolRequest, in TaskCreateInput) (*gomcp.CallToolResult, any, error) {
	if in.Content == "" {
		return toolError("content is required")
	}

	state := s.deps.Refs.Load()
	id := core.NewTaskID()

	newID, err := s.deps.Task.Create(ctx, id, in.Content, in.ContextIDs, state.Schema)
	if err != nil {
		return toolError(fmt.Sprintf("create task failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"status":  "ok",
		"id":      newID,
		"content": in.Content,
	})
}

// handleTaskList lists tasks via the task service.
func (s *Server) handleTaskList(ctx context.Context, _ *gomcp.CallToolRequest, in TaskListInput) (*gomcp.CallToolResult, any, error) {
	status := ""
	if in.Status != nil {
		status = *in.Status
	}
	goalID := ""
	if in.GoalID != nil {
		goalID = *in.GoalID
	}

	state := s.deps.Refs.Load()
	tasks, err := s.deps.Task.List(ctx, status, goalID, state.Schema)
	if err != nil {
		return toolError(fmt.Sprintf("list tasks failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// handleTaskStatus transitions a task status via the task service.
func (s *Server) handleTaskStatus(ctx context.Context, _ *gomcp.CallToolRequest, in TaskStatusInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" || in.Status == "" {
		return toolError("id and status are required")
	}

	state := s.deps.Refs.Load()
	err := s.deps.Task.Status(ctx, in.ID, in.Status, state.Schema)
	if err != nil {
		return toolError(fmt.Sprintf("status transition failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"id":     in.ID,
		"status": in.Status,
	})
}

// handleTaskShow shows task details via the task service.
func (s *Server) handleTaskShow(ctx context.Context, _ *gomcp.CallToolRequest, in TaskShowInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" {
		return toolError("id is required")
	}

	state := s.deps.Refs.Load()
	entity, blocked, recovers, err := s.deps.Task.Show(ctx, in.ID, state.Schema)
	if err != nil {
		return toolError(fmt.Sprintf("show task failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"entity":       entity,
		"blocked_by":   blocked,
		"recovers_via": recovers,
	})
}

// handleGraphComponents finds connected components via the graph service.
func (s *Server) handleGraphComponents(ctx context.Context, _ *gomcp.CallToolRequest, in GraphComponentsInput) (*gomcp.CallToolResult, any, error) {
	minSize := 2
	if in.MinSize != nil && *in.MinSize > 0 {
		minSize = *in.MinSize
	}

	components, err := s.deps.Graph.Components(ctx, minSize)
	if err != nil {
		return toolError(fmt.Sprintf("graph components failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"components": components,
		"count":      len(components),
	})
}

// handleIngestDialog ingests a conversation dialog via the ingest service.
func (s *Server) handleIngestDialog(ctx context.Context, _ *gomcp.CallToolRequest, in IngestDialogInput) (*gomcp.CallToolResult, any, error) {
	if in.Dialog == "" {
		return toolError("dialog is required")
	}

	dedupThreshold := float32(0.8)
	state := s.deps.Refs.Load()
	if state != nil && s.deps.Ingest != nil {
		// Use a default dedup threshold; the HTTP server reads this from config.
		_ = dedupThreshold
	}

	err := s.deps.Ingest.Ingest(ctx, in.Dialog, dedupThreshold, s.schema())
	if err != nil {
		return toolError(fmt.Sprintf("ingest failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"status": "ok",
	})
}

// handleGraphCommunities runs Louvain community detection via the graph service.
func (s *Server) handleGraphCommunities(ctx context.Context, _ *gomcp.CallToolRequest, in GraphCommunitiesInput) (*gomcp.CallToolResult, any, error) {
	maxIter := 10
	if in.MaxIterations != nil && *in.MaxIterations > 0 {
		maxIter = *in.MaxIterations
	}

	communities, globalQ, err := s.deps.Graph.Communities(ctx, maxIter)
	if err != nil {
		return toolError(fmt.Sprintf("graph communities failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"communities":       communities,
		"count":             len(communities),
		"global_modularity": globalQ,
	})
}

// handleGraphVerify runs graph integrity checks via the graph service.
func (s *Server) handleGraphVerify(ctx context.Context, _ *gomcp.CallToolRequest, _ GraphVerifyInput) (*gomcp.CallToolResult, any, error) {
	state := s.deps.Refs.Load()
	schema := core.DefaultSchemaConfig(false)
	dim := s.deps.VectorDim
	if state != nil {
		schema = state.Schema
	}

	report, err := s.deps.Graph.Verify(ctx, schema, dim)
	if err != nil {
		return toolError(fmt.Sprintf("graph verify failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"pass":   report.Pass(),
		"issues": report.Issues,
		"count":  len(report.Issues),
	})
}

// handleTaskTree renders the task dependency tree via the task service.
func (s *Server) handleTaskTree(ctx context.Context, _ *gomcp.CallToolRequest, in TaskTreeInput) (*gomcp.CallToolResult, any, error) {
	goalID := ""
	if in.GoalID != nil {
		goalID = *in.GoalID
	}

	state := s.deps.Refs.Load()
	schema := core.DefaultSchemaConfig(false)
	if state != nil {
		schema = state.Schema
	}

	tree, err := s.deps.Task.Tree(ctx, goalID, schema)
	if err != nil {
		return toolError(fmt.Sprintf("task tree failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"tree": tree,
	})
}

// handleTaskRollback cascade-rollbacks a failed task via the task service.
func (s *Server) handleTaskRollback(ctx context.Context, _ *gomcp.CallToolRequest, in TaskRollbackInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" {
		return toolError("id is required")
	}

	state := s.deps.Refs.Load()
	schema := core.DefaultSchemaConfig(false)
	if state != nil {
		schema = state.Schema
	}

	rolledBack, err := s.deps.Task.RollbackCascade(ctx, in.ID, in.ErrorContext, schema)

	result := map[string]interface{}{
		"rolled_back": rolledBack,
		"count":       len(rolledBack),
	}
	if err != nil {
		result["error"] = err.Error()
		result["partial"] = true
	}
	return outputJSON(result)
}

// handleGraphContradictions lists contradiction edges via the contradiction service.
func (s *Server) handleGraphContradictions(ctx context.Context, _ *gomcp.CallToolRequest, in GraphContradictionsInput) (*gomcp.CallToolResult, any, error) {
	entityID := ""
	if in.ID != nil {
		entityID = *in.ID
	}

	if s.deps.Contradictions == nil {
		return toolError("contradiction service not available")
	}

	pairs, err := s.deps.Contradictions.List(ctx, entityID)
	if err != nil {
		return toolError(fmt.Sprintf("graph contradictions failed: %v", err))
	}

	return outputJSON(map[string]interface{}{
		"pairs": pairs,
		"count": len(pairs),
	})
}

// handleMemoryExplain returns a ScoreBreakdown for a single memory node by ID.
func (s *Server) handleMemoryExplain(ctx context.Context, _ *gomcp.CallToolRequest, in MemoryExplainInput) (*gomcp.CallToolResult, any, error) {
	if in.ID == "" {
		return toolError("id is required")
	}

	queryText := ""
	if in.Query != nil {
		queryText = *in.Query
	}

	breakdown, err := s.deps.Retrieve.ExplainNode(ctx, in.ID, queryText)
	if err != nil {
		return toolError(fmt.Sprintf("memory explain failed: %v", err))
	}

	if breakdown == nil {
		return toolError(fmt.Sprintf("entity not found: %s", in.ID))
	}

	return outputJSON(map[string]interface{}{
		"id":              in.ID,
		"score_breakdown": breakdown,
	})
}
