package retrieval

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/tracing"
)

func tracerFromOpts(opts core.RetrieveContextOptions) tracing.Tracer {
	if opts.Ctx != nil {
		return tracing.TracerFrom(opts.Ctx)
	}
	return tracing.NoopTracer{}
}

func spanFromOpts(opts core.RetrieveContextOptions, name string) (context.Context, tracing.Span) {
	t := tracerFromOpts(opts)
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return t.StartSpan(ctx, name)
}

// startStageSpan opens a span for a single retrieval pipeline stage
// and returns it without the new context. Retrieval stages currently
// don't consume ctx (they read opts.Ctx internally for cancellation),
// so callers can ignore the ctx returned by spanFromOpts and use this
// helper instead. Errors should be recorded on the span before End.
//
// Span name is prefixed with "retrieval." so it nests under any
// outer /retrieve handler span in the OTLP tree.
func startStageSpan(opts core.RetrieveContextOptions, name string) tracing.Span {
	_, span := spanFromOpts(opts, "retrieval."+name)
	return span
}
