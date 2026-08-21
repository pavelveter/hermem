package retrieval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Renderer renders a RetrievalResult to a specific output format.
type Renderer interface {
	Render(result *RetrievalResult) string
}

// FactFormatter formats a single RetrievedFact for display.
type FactFormatter func(f RetrievedFact) string

// traverseBuckets iterates over the four fact categories and calls fn for each.
func traverseBuckets(result *RetrievalResult, fn func(category string, facts []RetrievedFact)) {
	if result == nil || len(result.SeedNodes) == 0 {
		return
	}
	fn("WORLD", result.WorldFacts)
	fn("OPINION", result.Opinions)
	fn("EXPERIENCE", result.Experiences)
	fn("OBSERVATION", result.Observations)
}

// MarkdownRenderer renders a RetrievalResult as markdown.
type MarkdownRenderer struct{}

// Render implements Renderer.
func (r *MarkdownRenderer) Render(result *RetrievalResult) string {
	var sb strings.Builder
	sb.WriteString("# Memory Context\n\n")
	traverseBuckets(result, func(cat string, facts []RetrievedFact) {
		writeBucket(&sb, cat, facts)
	})
	return sb.String()
}

// PlainTextRenderer renders a RetrievalResult as plain text.
type PlainTextRenderer struct{}

// Render implements Renderer.
func (r *PlainTextRenderer) Render(result *RetrievalResult) string {
	var sb strings.Builder
	traverseBuckets(result, func(cat string, facts []RetrievedFact) {
		writePlainTextBucket(&sb, cat, facts)
	})
	return sb.String()
}

// JSONRenderer renders a RetrievalResult using encoding/json.
type JSONRenderer struct{}

// Render implements Renderer.
func (r *JSONRenderer) Render(result *RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return "{}"
	}
	out := map[string][]string{
		"world_facts":  renderFactsJSON(result.WorldFacts),
		"opinions":     renderFactsJSON(result.Opinions),
		"experiences":  renderFactsJSON(result.Experiences),
		"observations": renderFactsJSON(result.Observations),
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func renderFactsJSON(facts []RetrievedFact) []string {
	if len(facts) == 0 {
		return nil
	}
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		out = append(out, f.Content)
	}
	return out
}

func writeBucket(sb *strings.Builder, heading string, facts []RetrievedFact) {
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

func writePlainTextBucket(sb *strings.Builder, heading string, facts []RetrievedFact) {
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
