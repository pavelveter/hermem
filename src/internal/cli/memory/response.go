package memory

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newResponseCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "response",
		Short: "Generate an LLM response using retrieved graph context as evidence",
		Long: `Generate an LLM response grounded in knowledge graph context.

Input (JSON on stdin):
  {
    "query": "user question",
    "max_depth": 3                       // optional, graph walk depth
  }

Pipeline:
  1. Run the full retrieval pipeline (embed → search → graph walk).
  2. Build a context blob from retrieved entities.
  3. Send the context + query to the configured LLM (extractor).
  4. Return the LLM's response.

This is the end-to-end "ask a question, get an answer" command.
The response is grounded in the knowledge graph — citations come from
stored entities, not hallucinated.

Requires both an embedder and an extractor to be configured.

Output (JSON):
  {"response": "LLM-generated answer text"}

Examples:
  echo '{"query":"What is the project architecture?"}' | hermem memory response
  echo '{"query":"List all known bugs","max_depth":3}' | hermem memory response | jq -r '.response'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Query    string `json:"query"`
				MaxDepth int    `json:"max_depth,omitempty"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Query == "" {
				return fmt.Errorf("query is required")
			}
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				TokenBudget:       env.Cfg.TokenBudget,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
				Ctx:               env.Ctx,
			}
			if req.MaxDepth > 0 {
				opts.MaxDepth = req.MaxDepth
			}
			out, err := svc.Response(env.Ctx, req.Query, opts)
			if err != nil {
				return fmt.Errorf("response: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"response": out})
		},
	}
}
