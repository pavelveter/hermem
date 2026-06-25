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
