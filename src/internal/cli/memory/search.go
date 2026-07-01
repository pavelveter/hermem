package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newSearchCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "search",
		Short: "Top-K nearest neighbors by query embedding",
		Long: `Perform vector similarity search against stored embeddings.

Input (JSON on stdin):
  {
    "query": "natural language query",
    "top_k": 10                          // optional, default 10
  }

The query text is embedded using the configured embedder, then the
top-K nearest neighbors are returned by cosine similarity. Results
include entity ID, category, content, and similarity score.

Output (JSON):
  [{"entity":{...}, "score":0.92}, ...]

Use "hermem memory query" for the full pipeline (embed → search →
graph walk → Markdown context). Use this command when you only need
raw vector search results.

Examples:
  echo '{"query":"functional programming","top_k":5}' | hermem memory search
  echo '{"query":"deployment checklist"}' | hermem memory search | jq '.[].entity.id'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Query string `json:"query"`
				TopK  int    `json:"top_k,omitempty"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Query == "" {
				return fmt.Errorf("query required")
			}
			// Construct per-call (three pointer assignments; cheap) so
			// CLI never holds a stale Service pointer between commands.
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			results, err := svc.Search(env.Ctx, req.Query, req.TopK)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
		},
	}
}
