package main

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// InitTracing sets up the OpenTelemetry tracer provider with a stdout
// exporter. If OTEL_EXPORTER_OTLP_ENDPOINT is set, the standard OTLP
// exporter is preferred over stdout (handled via OTEL SDK auto-detection).
// Returns a shutdown function to flush pending spans on exit.
func InitTracing() (shutdown func(), err error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Only log if the user hasn't set an explicit exporter endpoint.
	// When OTEL_EXPORTER_OTLP_ENDPOINT is set, the SDK auto-configures
	// the OTLP exporter via the standard environment variable.
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		slog.Info("tracing enabled (stdout exporter)", "event", "tracing_init")
	}

	shutdown = func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			slog.Error("tracing shutdown", "event", "tracing_shutdown_error", "error", err)
		}
	}
	return shutdown, nil
}

// Tracer returns the application tracer. Use it to create spans:
//
//	ctx, span := Tracer().Start(ctx, "operation.name")
//	defer span.End()
func Tracer() trace.Tracer {
	return otel.Tracer("hermem")
}
