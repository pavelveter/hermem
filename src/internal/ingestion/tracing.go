package ingestion

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/tracing"
)

func spanStart(ctx context.Context, name string) (context.Context, tracing.Span) {
	t := tracing.TracerFrom(ctx)
	return t.StartSpan(ctx, name)
}

func ProcessDialogWithTracing(ctx context.Context, w *IngestionWorker, dialog string) error {
	ctx, span := spanStart(ctx, "ingestion.ProcessDialog")
	defer span.End()
	err := w.ProcessDialog(ctx, dialog)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func ProcessDialogWithProvenanceWithTracing(ctx context.Context, w *IngestionWorker, dialog string, prov core.Provenance) error {
	ctx, span := spanStart(ctx, "ingestion.ProcessDialogWithProvenance")
	defer span.End()
	err := w.ProcessDialogWithProvenance(ctx, dialog, prov)
	if err != nil {
		span.RecordError(err)
	}
	return err
}
