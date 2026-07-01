package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	ingestdomain "github.com/pavelveter/hermem/src/internal/ingest"
)

func newIngestCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Drain a dialog through the LLM extractor and ingest extracted facts",
		Long: `Ingest a conversation dialog by extracting facts via the LLM extractor.

Input (JSON on stdin):
  {
    "dialog": "multi-line conversation text or any free-form text"
  }

Pipeline:
  1. Send the dialog to the configured LLM extractor.
  2. Extract facts, opinions, experiences, and observations.
  3. Embed each extracted fact.
  4. Deduplicate against existing entities (threshold from config).
  5. Detect contradictions with existing knowledge.
  6. Persist new entities and edges to the knowledge graph.
  7. Update community clusters.

The dedup threshold (hermem.ini [memory] dedup_threshold) controls
how similar two entities must be to be considered duplicates.

Output:
  {"status":"ok"}

Examples:
  echo '{"dialog":"Alice: Go is fast. Bob: Yes, and it has garbage collection."}' | hermem memory ingest
  cat conversation.txt | python3 -c "import sys,json; print(json.dumps({'dialog':sys.stdin.read()}))" | hermem memory ingest`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.IngestRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Dialog == "" {
				return fmt.Errorf("dialog required")
			}
			ingestSvc := ingestdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
			if err := ingestSvc.Ingest(env.Ctx, req.Dialog, env.Cfg.DedupThreshold, env.Cfg.Schema); err != nil {
				return fmt.Errorf("ingest: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
