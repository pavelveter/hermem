package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestNewSlogLogger(t *testing.T) {
	var buf bytes.Buffer
	l := NewSlogLogger(slog.LevelInfo, &buf)
	if l == nil {
		t.Fatal("NewSlogLogger returned nil")
	}
	l.Info("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected log output to contain 'test message', got: %s", buf.String())
	}
}

func TestNewDefaultLogger(t *testing.T) {
	l := NewDefaultLogger()
	if l == nil {
		t.Fatal("NewDefaultLogger returned nil")
	}
	// Should not panic
	l.Info("default logger test")
}

func TestSlogOrFallback(t *testing.T) {
	// With nil, should return a fallback
	fallback := SlogOrFallback(nil)
	if fallback == nil {
		t.Fatal("SlogOrFallback(nil) returned nil")
	}

	// With a real logger, should return it
	var buf bytes.Buffer
	real := NewSlogLogger(slog.LevelInfo, &buf)
	result := SlogOrFallback(real)
	if result != real {
		t.Error("SlogOrFallback should return the provided logger")
	}
}

func TestPrefixedLogger(t *testing.T) {
	var buf bytes.Buffer
	l := NewSlogLogger(slog.LevelInfo, &buf)
	prefixed := PrefixedLogger{Logger: l, Component: "test"}

	prefixed.Info("hello")
	if !strings.Contains(buf.String(), "[test]") {
		t.Errorf("expected prefix [test] in output, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("expected 'hello' in output, got: %s", buf.String())
	}
}

func TestPrefixedLoggerLevels(t *testing.T) {
	tests := []struct {
		name   string
		level  slog.Level
		fn     func(core.Logger, string, ...any)
		expect string
	}{
		{"debug", slog.LevelDebug, func(l core.Logger, msg string, args ...any) { l.Debug(msg, args...) }, "DEBUG"},
		{"info", slog.LevelInfo, func(l core.Logger, msg string, args ...any) { l.Info(msg, args...) }, "INFO"},
		{"warn", slog.LevelWarn, func(l core.Logger, msg string, args ...any) { l.Warn(msg, args...) }, "WARN"},
		{"error", slog.LevelError, func(l core.Logger, msg string, args ...any) { l.Error(msg, args...) }, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := NewSlogLogger(tt.level, &buf)
			prefixed := PrefixedLogger{Logger: l, Component: "t"}
			tt.fn(prefixed, "msg")
			if !strings.Contains(buf.String(), tt.expect) {
				t.Errorf("expected %s in output, got: %s", tt.expect, buf.String())
			}
		})
	}
}

func TestPrefixedLoggerPrefixed(t *testing.T) {
	l := PrefixedLogger{Component: "svc"}
	result := l.prefixed("message")
	if result != "[svc] message" {
		t.Errorf("expected '[svc] message', got: %q", result)
	}
}
