package core

// Logger is the unified logging interface for all herm layers.
// It mirrors the slog signature pattern (msg string, args ...any)
// so migration from global slog is a mechanical rename.
//
// Every service constructor SHOULD accept a Logger as its last parameter
// and default to a slog-based implementation when nil is passed.
// During the migration, the global slog default logger remains functional;
// call sites are converted incrementally.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NoopLogger is a Logger that discards all messages.
// Useful for tests that don't need log output.
type NoopLogger struct{}

func (NoopLogger) Debug(string, ...any) {}
func (NoopLogger) Info(string, ...any)  {}
func (NoopLogger) Warn(string, ...any)  {}
func (NoopLogger) Error(string, ...any) {}
