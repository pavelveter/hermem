package main

import (
	"database/sql"
	"fmt"
	"strings"
)

type GraphNode struct {
	Entity       Entity
	Relations    []Edge
	Depth        int
	ParentID     string
	RelationType string
}

type RetrievalResult struct {
	SeedNodes    []GraphNode
	WorldFacts   []string
	Opinions     []string
	Experiences  []string
	Observations []string
}

// RetrieveContextOptions controls graph-walk bounds for a single
// RetrieveContext call. All fields are optional (zero values are safe
// and mean "use the library defaults / no cap").
type RetrieveContextOptions struct {
	// MaxDepth is the caller's requested depth (<=0 defaults to 2).
	// The actual walk uses min(MaxDepth, DepthCeiling).
	MaxDepth int
	// DepthCeiling clamps MaxDepth; <=0 disables the clamp.
	DepthCeiling int
	// MaxRetrievedNodes soft-caps the total unique entities returned;
	// <=0 disables. May be exceeded by at most one row because the cap
	// is checked after seenIDs updates the running count.
	MaxRetrievedNodes int
}

func RetrieveContext(db *sql.DB, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error) {
	if len(seedIDs) == 0 {
		return &RetrievalResult{}, nil
	}

	effectiveDepth := opts.MaxDepth
	if effectiveDepth <= 0 {
		effectiveDepth = 2
	}
	if opts.DepthCeiling > 0 && effectiveDepth > opts.DepthCeiling {
		effectiveDepth = opts.DepthCeiling
	}

	placeholders := make([]string, len(seedIDs))
	args := make([]interface{}, len(seedIDs))
	for i, id := range seedIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	args = append(args, effectiveDepth)

	query := fmt.Sprintf(`
		WITH RECURSIVE graph_walk AS (
			SELECT 
				e.id, e.category, e.content, e.updated_at,
				0 as depth,
				'' as parent_id,
				'' as relation_type
			FROM entities e
			WHERE e.id IN (%s)
			
			UNION ALL
			
			SELECT 
				e.id, e.category, e.content, e.updated_at,
				gw.depth + 1,
				gw.id as parent_id,
				ed.relation_type
			FROM graph_walk gw
			JOIN edges ed ON (ed.source_id = gw.id OR ed.target_id = gw.id)
			JOIN entities e ON (
				CASE 
					WHEN ed.source_id = gw.id THEN ed.target_id = e.id
					ELSE ed.source_id = e.id
				END
			)
			WHERE gw.depth < ? AND e.id != gw.id
		)
		SELECT DISTINCT id, category, content, updated_at, depth, parent_id, relation_type
		FROM graph_walk
		ORDER BY depth ASC, category ASC
	`, strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute graph retrieval: %w", err)
	}
	defer rows.Close()

	result := &RetrievalResult{
		SeedNodes:    []GraphNode{},
		WorldFacts:   []string{},
		Opinions:     []string{},
		Experiences:  []string{},
		Observations: []string{},
	}

	seenIDs := make(map[string]bool)
	seenContents := make(map[string]bool)

	for rows.Next() {
		var node GraphNode
		if err := rows.Scan(
			&node.Entity.ID,
			&node.Entity.Category,
			&node.Entity.Content,
			&node.Entity.UpdatedAt,
			&node.Depth,
			&node.ParentID,
			&node.RelationType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan graph node: %w", err)
		}

		if !seenIDs[node.Entity.ID] {
			seenIDs[node.Entity.ID] = true
		} else {
			continue
		}

		// Soft cap: stop scanning once we've collected MaxRetrievedNodes
		// unique entities. The check fires after seenIDs updates the
		// running count but BEFORE the row is added to SeedNodes or the
		// category buckets, so the output is bounded at exactly N entities
		// (the trigger row is dropped). The residue seenIDs entry is local
		// to this function and never escapes.
		if opts.MaxRetrievedNodes > 0 && len(seenIDs) > opts.MaxRetrievedNodes {
			break
		}

		if node.Depth == 0 {
			result.SeedNodes = append(result.SeedNodes, node)
		}

		if seenContents[node.Entity.Content] {
			continue
		}
		seenContents[node.Entity.Content] = true

		switch node.Entity.Category {
		case "world":
			result.WorldFacts = append(result.WorldFacts, node.Entity.Content)
		case "opinion":
			result.Opinions = append(result.Opinions, node.Entity.Content)
		case "experience":
			result.Experiences = append(result.Experiences, node.Entity.Content)
		case "observation":
			result.Observations = append(result.Observations, node.Entity.Content)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating graph rows: %w", err)
	}

	return result, nil
}

func FormatContextMarkdown(result *RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Hindsight Context\n\n")

	if len(result.WorldFacts) > 0 {
		sb.WriteString("## WORLD\n")
		for _, fact := range result.WorldFacts {
			sb.WriteString(fmt.Sprintf("- %s\n", fact))
		}
		sb.WriteString("\n")
	}

	if len(result.Opinions) > 0 {
		sb.WriteString("## OPINION\n")
		for _, opinion := range result.Opinions {
			sb.WriteString(fmt.Sprintf("- %s\n", opinion))
		}
		sb.WriteString("\n")
	}

	if len(result.Experiences) > 0 {
		sb.WriteString("## EXPERIENCE\n")
		for _, exp := range result.Experiences {
			sb.WriteString(fmt.Sprintf("- %s\n", exp))
		}
		sb.WriteString("\n")
	}

	if len(result.Observations) > 0 {
		sb.WriteString("## OBSERVATION\n")
		for _, obs := range result.Observations {
			sb.WriteString(fmt.Sprintf("- %s\n", obs))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
