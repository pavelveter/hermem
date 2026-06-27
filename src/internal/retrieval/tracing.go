package retrieval

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/tracing"
)

// startStageSpan opens a span for a single retrieval pipeline stage
// and returns it without the new context. Retrieval stages currently
// don't consume ctx (they read opts.Ctx internally for cancellation),
// so callers can ignore the ctx returned by the span creation.
// Errors should be recorded on the span before End.
//
// Span name is prefixed with "retrieval." so it nests under any
// outer /retrieve handler span in the OTLP tree.
func startStageSpan(opts core.RetrieveContextOptions, name string) tracing.Span {
	var t tracing.Tracer
	if opts.Ctx != nil {
		t = tracing.TracerFrom(opts.Ctx)
	} else {
		t = tracing.NoopTracer{}
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := t.StartSpan(ctx, "retrieval."+name)
	return span
}
