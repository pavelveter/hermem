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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req core.IngestRequest
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			// Pre-validation kept at CLI for verbatim message parity.
			if req.Dialog == "" {
				return fmt.Errorf("dialog required")
			}
			// PHASE 3.4: ingest orchestration moved to ingest.Service.Ingest;
			// memory.Service.Ingest removed in PHASE 3.4. The dialog pipeline
			// body is unchanged — only the domain Service constructor pointer.
			ingestSvc := ingestdomain.NewService(env.DB, env.VI, env.Embedder, env.Extractor)
			if err := ingestSvc.Ingest(env.Ctx, req.Dialog, env.Cfg.DedupThreshold, env.Cfg.Schema); err != nil {
				return fmt.Errorf("ingest: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok"}`)
			return nil
		},
	}
}
