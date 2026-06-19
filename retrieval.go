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
	SeedNodes   []GraphNode
	WorldFacts  []string
	Opinions    []string
	Experiences []string
	Observations []string
}

func RetrieveContext(db *sql.DB, seedIDs []string, maxDepth int) (*RetrievalResult, error) {
	if len(seedIDs) == 0 {
		return &RetrievalResult{}, nil
	}

	if maxDepth <= 0 {
		maxDepth = 2
	}

	placeholders := make([]string, len(seedIDs))
	args := make([]interface{}, len(seedIDs))
	for i, id := range seedIDs {
		placeholders[i] = "?"
		args[i] = id
	}

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
			FROM entities e
			JOIN edges ed ON (
				(ed.source_id = gw.id AND ed.target_id = e.id) OR 
				(ed.target_id = gw.id AND ed.source_id = e.id)
			)
			JOIN graph_walk gw ON 1=1
			WHERE gw.depth < ? AND e.id != gw.id
		)
		SELECT DISTINCT
			id, category, content, updated_at,
			depth, parent_id, relation_type
		FROM graph_walk
		ORDER BY depth, category, id
	`, strings.Join(placeholders, ","))

	args = append(args, maxDepth)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute graph walk: %w", err)
	}
	defer rows.Close()

	result := &RetrievalResult{}
	seenIDs := make(map[string]bool)

	for rows.Next() {
		var node GraphNode
		if err := rows.Scan(
			&node.Entity.ID, &node.Entity.Category, &node.Entity.Content,
			&node.Entity.UpdatedAt, &node.Depth, &node.ParentID, &node.RelationType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan graph node: %w", err)
		}

		if seenIDs[node.Entity.ID] {
			continue
		}
		seenIDs[node.Entity.ID] = true

		result.SeedNodes = append(result.SeedNodes, node)

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
		return nil, fmt.Errorf("error iterating rows: %w", err)
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
