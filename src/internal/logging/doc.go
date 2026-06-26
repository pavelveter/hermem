// Package logging provides core.Logger implementations and test utilities.
//
// Implementations:
//   - SlogLogger: production adapter wrapping slog.Handler (JSON to stderr/file).
//   - TestLogger: in-memory capture for test assertions.
//
// Migration path:
// Services accept core.Logger as their last constructor parameter.
// When nil, default to logging.SlogOrFallback(nil) which delegates to
// the global slog default logger — existing call sites continue working
// through the transition.
package logging
