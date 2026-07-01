// Package logging provides Logger implementations and testing utilities.
package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/pavelveter/hermem/src/internal/core"
)

// SlogLogger adapts slog.Handler to the core.Logger interface.
// It is the production default — zero-allocation wrappers over the
// standard library structured logger.
type SlogLogger struct {
	logger *slog.Logger
}

// NewSlogLogger creates a Logger that writes JSON to w at the given level.
// Pass nil for w to write to stderr (same as slog default).
func NewSlogLogger(level slog.Level, w io.Writer) *SlogLogger {
	if w == nil {
		w = os.Stderr
	}
	return &SlogLogger{
		logger: slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})),
	}
}

// NewDefaultLogger returns a Logger using the global slog default logger.
// This is the transitional default — services that haven't been migrated
// to DI yet can use this to get a core.Logger that delegates to the
// global slog instance.
func NewDefaultLogger() *SlogLogger {
	return &SlogLogger{logger: slog.Default()}
}

// SlogOrFallback returns l if non-nil, otherwise a SlogLogger wrapping
// the global slog default. Intended for constructor defaulting:
//
//	if l == nil { l = logging.SlogOrFallback(l) }
func SlogOrFallback(l core.Logger) core.Logger {
	if l != nil {
		return l
	}
	return NewDefaultLogger()
}

// PrefixedLogger wraps a core.Logger and prepends "[component]" to every
// message. Use it to give each service its own logger identity without
// duplicating the underlying handler:
//
//	logger := logging.PrefixedLogger{Logger: l, Component: "task"}
//	logger.Info("created", "id", id) // [task] created id=t-1
type PrefixedLogger struct {
	Logger    core.Logger
	Component string
}

func (l PrefixedLogger) prefixed(msg string) string {
	return "[" + l.Component + "] " + msg
}

func (l PrefixedLogger) Debug(msg string, args ...any) { l.Logger.Debug(l.prefixed(msg), args...) }
func (l PrefixedLogger) Info(msg string, args ...any)  { l.Logger.Info(l.prefixed(msg), args...) }
func (l PrefixedLogger) Warn(msg string, args ...any)  { l.Logger.Warn(l.prefixed(msg), args...) }
func (l PrefixedLogger) Error(msg string, args ...any) { l.Logger.Error(l.prefixed(msg), args...) }

// Debug logs at Debug level.
func (l *SlogLogger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// Info logs at Info level.
func (l *SlogLogger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn logs at Warn level.
func (l *SlogLogger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error logs at Error level.
func (l *SlogLogger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}
