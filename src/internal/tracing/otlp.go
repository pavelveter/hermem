package tracing

import (
	"log/slog"
	"os"
)

const envExporter = "TRACING_EXPORTER"

func NewTracerFromEnv() Tracer {
	switch os.Getenv(envExporter) {
	case "otlp":
		slog.Info("TRACING_EXPORTER=otlp set — otel SDK not wired; falling back to NoopTracer")
		return NoopTracer{}
	default:
		return NoopTracer{}
	}
}
