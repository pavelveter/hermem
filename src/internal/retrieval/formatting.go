package retrieval

import (
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// FormatContextMarkdown renders a RetrievalResult as markdown, grouped by category.
func FormatContextMarkdown(result *core.RetrievalResult) string {
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

func writeBucket(sb *strings.Builder, heading string, facts []core.RetrievedFact) {
	if len(facts) == 0 {
		return
	}
	sb.WriteString("## " + heading + "\n")
	for _, f := range facts {
		if f.Depth > 0 && f.ParentID != "" {
			sb.WriteString(fmt.Sprintf("- %s (via '%s' from %s)\n", f.Content, f.RelationType, f.ParentID))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", f.Content))
		}
	}
	sb.WriteString("\n")
}
