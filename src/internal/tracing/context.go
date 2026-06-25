package tracing

import (
	"context"
)

type reqIDKey struct{}

func WithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

func SpanFrom(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey{}).(Span); ok {
		return s
	}
	return NoopSpan{}
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDKey{}, id)
}

func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey{}).(string); ok {
		return id
	}
	return ""
}

func StartSpan(ctx context.Context, tracer Tracer, name string) (context.Context, Span) {
	return tracer.StartSpan(ctx, name)
}
