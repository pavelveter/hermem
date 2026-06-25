package tracing

import (
	"context"
	"testing"
)

func TestNoopTracer_ImplementsTracer(t *testing.T) {
	var _ Tracer = NoopTracer{}
}

func TestNoopSpan_ImplementsSpan(t *testing.T) {
	var _ Span = NoopSpan{}
}

func TestNoopTracer_StartSpanReturnsContext(t *testing.T) {
	tracer := NoopTracer{}
	ctx, span := tracer.StartSpan(context.Background(), "test")
	if span == nil {
		t.Fatal("StartSpan returned nil span")
	}
	span.End()
	span.SetAttribute("key", "value")
	span.RecordError(nil)
	// Verify span can be extracted from context
	got := SpanFrom(ctx)
	if got == nil {
		t.Fatal("SpanFrom returned nil")
	}
}

func TestSpanFrom_ReturnsNoopOnMissing(t *testing.T) {
	span := SpanFrom(context.Background())
	if _, ok := span.(NoopSpan); !ok {
		t.Fatalf("want NoopSpan, got %T", span)
	}
}

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	if id := RequestIDFrom(ctx); id != "req-123" {
		t.Fatalf("want req-123, got %q", id)
	}
}

func TestRequestID_EmptyWhenMissing(t *testing.T) {
	if id := RequestIDFrom(context.Background()); id != "" {
		t.Fatalf("want empty, got %q", id)
	}
}

func TestWithSpan_RoundTrip(t *testing.T) {
	ctx, span := NoopTracer{}.StartSpan(context.Background(), "test")
	ctx2 := WithSpan(ctx, span)
	got := SpanFrom(ctx2)
	if got != span {
		t.Fatal("WithSpan/SpanFrom roundtrip failed")
	}
}

func TestNewTracerFromEnv_DefaultNoop(t *testing.T) {
	tracer := NewTracerFromEnv()
	if _, ok := tracer.(NoopTracer); !ok {
		t.Fatalf("want NoopTracer, got %T", tracer)
	}
}
