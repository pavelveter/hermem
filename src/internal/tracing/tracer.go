package tracing

import (
	"context"
)

type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

type Span interface {
	End()
	SetAttribute(key string, value any)
	RecordError(err error)
}

type NoopTracer struct{}

func (NoopTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	span := NoopSpan{}
	return context.WithValue(ctx, spanKey{}, span), span
}

type NoopSpan struct{}

func (NoopSpan) End()                     {}
func (NoopSpan) SetAttribute(string, any) {}
func (NoopSpan) RecordError(error)        {}

type spanKey struct{}
