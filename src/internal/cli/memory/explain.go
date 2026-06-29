package memory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

func newExplainCmd(env *cli.Env) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain the reasoning path from query to retrieved entities",
		Args:  cobra.NoArgs,
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
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			opts := core.RetrieveContextOptions{
				MaxDepth:          2,
				DepthCeiling:      env.Cfg.MaxDepthCeiling,
				MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
				TokenBudget:       env.Cfg.TokenBudget,
				QueryText:         req.Query,
				Ctx:               env.Ctx,
				Explain:           true,
				RankingWeight:     env.Cfg.Ranking,
				Reranker:          env.Reranker,
			}
			result, err := svc.Explain(env.Ctx, req.Query, req.TopK, opts)
			if err != nil {
				return fmt.Errorf("explain: %w", err)
			}

			if jsonOutput {
				return printJSON(cmd, result)
			}
			return printExplainTree(cmd, req.Query, result, env)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw JSON instead of ASCII tree")
	return cmd
}

func printExplainTree(cmd *cobra.Command, query string, result *core.RetrievalResult, env *cli.Env) error {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Query: %q\n\n", query)

	// Seeds section.
	sb.WriteString("── Seeds (vector search) ──\n")
	if len(result.SeedNodes) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, seed := range result.SeedNodes {
			sim := "?"
			if env.VI != nil {
				sim = "≈"
			}
			fmt.Fprintf(&sb, "  [%s] depth=0 score=%.3f %s\n",
				seed.Entity.ID, seed.RankingScore, sim)
		}
	}
	sb.WriteString("\n")

	// Graph walk section.
	sb.WriteString("── Graph walk (edges traversed) ──\n")
	allFacts := collectAllFacts(result)
	if len(allFacts) == 0 {
		sb.WriteString("  (no facts retrieved)\n")
	} else {
		for _, f := range allFacts {
			depthStr := fmt.Sprintf("d=%d", f.Depth)
			parentStr := ""
			if f.ParentID != "" {
				parentStr = fmt.Sprintf(" via '%s' from %s", f.RelationType, f.ParentID)
			}
			scoreStr := ""
			if f.ScoreBreakdown != nil {
				scoreStr = fmt.Sprintf(" [score=%.3f vec=%.3f rec=%.3f cent=%.3f depth=%.3f]",
					f.ScoreBreakdown.FinalScore,
					f.ScoreBreakdown.VectorScore,
					f.ScoreBreakdown.RecencyScore,
					f.ScoreBreakdown.CentralityScore,
					f.ScoreBreakdown.DepthPenalty)
			}
			fmt.Fprintf(&sb, "  %s %-12s%s%s\n",
				indent(f.Depth), depthStr, truncate(f.Content, 60), scoreStr)
			if parentStr != "" {
				fmt.Fprintf(&sb, "  %s└─%s\n", indent(f.Depth), parentStr)
			}
		}
	}
	sb.WriteString("\n")

	// Summary.
	sb.WriteString("── Summary ──\n")
	fmt.Fprintf(&sb, "  Seeds: %d | World: %d | Opinion: %d | Experience: %d | Observation: %d\n",
		len(result.SeedNodes),
		len(result.WorldFacts),
		len(result.Opinions),
		len(result.Experiences),
		len(result.Observations))

	_, err := fmt.Fprint(cmd.OutOrStdout(), sb.String())
	return err
}

func collectAllFacts(r *core.RetrievalResult) []core.RetrievedFact {
	var all []core.RetrievedFact
	all = append(all, r.WorldFacts...)
	all = append(all, r.Opinions...)
	all = append(all, r.Experiences...)
	all = append(all, r.Observations...)
	return all
}

func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func printJSON(cmd *cobra.Command, result *core.RetrievalResult) error {
	// Use vector's cosine similarity to show seed similarity if available.
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}
