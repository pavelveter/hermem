package retrieval

import (
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Renderer renders a RetrievalResult to a specific output format.
type Renderer interface {
	Render(result *core.RetrievalResult) string
}

// MarkdownRenderer renders a RetrievalResult as markdown.
type MarkdownRenderer struct{}

// Render implements Renderer.
func (r *MarkdownRenderer) Render(result *core.RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Memory Context\n\n")
	writeBucket(&sb, "WORLD", result.WorldFacts)
	writeBucket(&sb, "OPINION", result.Opinions)
	writeBucket(&sb, "EXPERIENCE", result.Experiences)
	writeBucket(&sb, "OBSERVATION", result.Observations)
	return sb.String()
}

// PlainTextRenderer renders a RetrievalResult as plain text.
type PlainTextRenderer struct{}

// Render implements Renderer.
func (r *PlainTextRenderer) Render(result *core.RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return ""
	}
	var sb strings.Builder
	writePlainTextBucket(&sb, "WORLD", result.WorldFacts)
	writePlainTextBucket(&sb, "OPINION", result.Opinions)
	writePlainTextBucket(&sb, "EXPERIENCE", result.Experiences)
	writePlainTextBucket(&sb, "OBSERVATION", result.Observations)
	return sb.String()
}

// JSONRenderer renders a RetrievalResult as a simple JSON-like text.
type JSONRenderer struct{}

// Render implements Renderer.
func (r *JSONRenderer) Render(result *core.RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return "{}"
	}
	var sb strings.Builder
	sb.WriteString("{\n")
	writeJSONBucket(&sb, "world_facts", result.WorldFacts, true)
	writeJSONBucket(&sb, "opinions", result.Opinions, true)
	writeJSONBucket(&sb, "experiences", result.Experiences, true)
	writeJSONBucket(&sb, "observations", result.Observations, false)
	sb.WriteString("}")
	return sb.String()
}

func writeBucket(sb *strings.Builder, heading string, facts []core.RetrievedFact) {
	if len(facts) == 0 {
		return
	}
	sb.WriteString("## " + heading + "\n")
	for _, f := range facts {
		if f.Depth > 0 && f.ParentID != "" {
			fmt.Fprintf(sb, "- %s (via '%s' from %s)\n", f.Content, f.RelationType, f.ParentID)
		} else {
			fmt.Fprintf(sb, "- %s\n", f.Content)
		}
	}
	sb.WriteString("\n")
}

func writePlainTextBucket(sb *strings.Builder, heading string, facts []core.RetrievedFact) {
	if len(facts) == 0 {
		return
	}
	sb.WriteString(heading + ":\n")
	for _, f := range facts {
		if f.Depth > 0 && f.ParentID != "" {
			fmt.Fprintf(sb, "  - %s (via '%s' from %s)\n", f.Content, f.RelationType, f.ParentID)
		} else {
			fmt.Fprintf(sb, "  - %s\n", f.Content)
		}
	}
	sb.WriteString("\n")
}

func writeJSONBucket(sb *strings.Builder, key string, facts []core.RetrievedFact, comma bool) {
	if len(facts) == 0 {
		return
	}
	fmt.Fprintf(sb, "  \"%s\": [\n", key)
	for i, f := range facts {
		fmt.Fprintf(sb, "    \"%s\"", f.Content)
		if i < len(facts)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	if comma {
		sb.WriteString("  ],\n")
	} else {
		sb.WriteString("  ]\n")
	}
}
