package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
)

func newIngestCmd(env cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Drain a dialog through the LLM extractor and ingest extracted facts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.IngestRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if req.Dialog == "" {
				return fmt.Errorf("dialog required")
			}
			w := ingestion.NewIngestionWorker(env.DB, env.VI, env.Extractor, env.Embedder, env.Cfg.DedupThreshold, env.Cfg.Schema)
			if err := w.ProcessDialog(env.Ctx, req.Dialog); err != nil {
				return fmt.Errorf("ingest: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
