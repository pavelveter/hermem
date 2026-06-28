package hermem

import (
	"context"
	"fmt"
	"net/url"
)

// GraphClient handles graph analytics and timeline operations.
type GraphClient struct {
	c *Client
}

// Verify returns graph integrity check results.
func (g *GraphClient) Verify(ctx context.Context) (*VerifyReport, error) {
	var result VerifyReport
	err := g.c.doGet(ctx, "/graph/verify", &result)
	return &result, err
}

// Contradictions returns all contradicts edges. Pass empty string for all.
func (g *GraphClient) Contradictions(ctx context.Context, entityID string) ([]ContradictionPair, error) {
	path := "/contradictions"
	if entityID != "" {
		path += "?id=" + url.QueryEscape(entityID)
	}
	var result []ContradictionPair
	err := g.c.doGet(ctx, path, &result)
	return result, err
}

// ConnectedComponents returns BFS-based connected components.
func (g *GraphClient) ConnectedComponents(ctx context.Context, minSize int) ([]ConnectedComponent, error) {
	path := "/connected-components"
	if minSize > 0 {
		path += fmt.Sprintf("?min_size=%d", minSize)
	}
	var result []ConnectedComponent
	err := g.c.doGet(ctx, path, &result)
	return result, err
}

// Communities returns Louvain community detection results.
func (g *GraphClient) Communities(ctx context.Context, minSize, maxIterations int) (map[string]interface{}, error) {
	path := "/communities"
	params := url.Values{}
	if minSize > 0 {
		params.Set("min_size", fmt.Sprintf("%d", minSize))
	}
	if maxIterations > 0 {
		params.Set("max_iterations", fmt.Sprintf("%d", maxIterations))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var result map[string]interface{}
	err := g.c.doGet(ctx, path, &result)
	return result, err
}

// Timeline returns recent entities ordered by created_at DESC.
func (g *GraphClient) Timeline(ctx context.Context, limit int) ([]TimelineEntry, error) {
	path := "/timeline"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var result []TimelineEntry
	err := g.c.doGet(ctx, path, &result)
	return result, err
}

// Provenance queries entities by memory origin.
func (g *GraphClient) Provenance(ctx context.Context, conversationID, messageID, source string, limit int) ([]Entity, error) {
	params := url.Values{}
	if conversationID != "" {
		params.Set("conversation_id", conversationID)
	}
	if messageID != "" {
		params.Set("message_id", messageID)
	}
	if source != "" {
		params.Set("source", source)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/provenance"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var result []Entity
	err := g.c.doGet(ctx, path, &result)
	return result, err
}

// RecoveryPlan walks recovers_via chain from a failed task.
func (g *GraphClient) RecoveryPlan(ctx context.Context, taskID string) ([]Entity, error) {
	path := "/recovery-plan?id=" + url.QueryEscape(taskID)
	var result []Entity
	err := g.c.doGet(ctx, path, &result)
	return result, err
}
