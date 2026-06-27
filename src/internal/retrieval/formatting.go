package retrieval

import (
	"github.com/pavelveter/hermem/src/internal/core"
)

// FormatContextMarkdown renders a RetrievalResult as markdown, grouped by category.
// Deprecated: Use MarkdownRenderer.Render() instead for the Renderer interface.
func FormatContextMarkdown(result *core.RetrievalResult) string {
	return (&MarkdownRenderer{}).Render(result)
}
