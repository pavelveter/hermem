package logging

import (
	"sync"

	"github.com/pavelveter/hermem/src/internal/core"
)

// CapturedMessage is a single log call recorded by TestLogger.
type CapturedMessage struct {
	Level   string // "debug", "info", "warn", "error"
	Message string
	Args    []any
}

// TestLogger captures every log call into an in-memory buffer
// for test assertions. Safe for concurrent use.
//
// Usage:
//
//	tl := logging.NewTestLogger()
//	svc := myservice.New(db, tl) // inject
//	svc.DoSomething()
//	msgs := tl.Messages()
//	assert.Contains(t, msgs[0].Message, "expected substring")
type TestLogger struct {
	mu       sync.Mutex
	messages []CapturedMessage
}

// NewTestLogger returns a Logger that records all calls.
func NewTestLogger() *TestLogger {
	return &TestLogger{}
}

// Debug records a debug-level message.
func (tl *TestLogger) Debug(msg string, args ...any) {
	tl.record("debug", msg, args)
}

// Info records an info-level message.
func (tl *TestLogger) Info(msg string, args ...any) {
	tl.record("info", msg, args)
}

// Warn records a warn-level message.
func (tl *TestLogger) Warn(msg string, args ...any) {
	tl.record("warn", msg, args)
}

// Error records an error-level message.
func (tl *TestLogger) Error(msg string, args ...any) {
	tl.record("error", msg, args)
}

// Messages returns a snapshot of all captured log calls.
func (tl *TestLogger) Messages() []CapturedMessage {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]CapturedMessage, len(tl.messages))
	copy(out, tl.messages)
	return out
}

// MessagesByLevel returns only messages at the given level.
func (tl *TestLogger) MessagesByLevel(level string) []CapturedMessage {
	all := tl.Messages()
	var out []CapturedMessage
	for _, m := range all {
		if m.Level == level {
			out = append(out, m)
		}
	}
	return out
}

// Reset clears the captured log buffer.
func (tl *TestLogger) Reset() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.messages = nil
}

func (tl *TestLogger) record(level, msg string, args []any) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	// Copy args to avoid aliasing with caller's mutable slice.
	argsCopy := make([]any, len(args))
	copy(argsCopy, args)
	tl.messages = append(tl.messages, CapturedMessage{
		Level:   level,
		Message: msg,
		Args:    argsCopy,
	})
}

// Compile-time check: TestLogger implements core.Logger.
var _ core.Logger = (*TestLogger)(nil)
