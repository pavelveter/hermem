package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// TimeoutMiddleware caps each request handler run at d. Inner of
// RecoveryMiddleware (a panic in a timeout-stalled handler still
// produces 500), outer of all per-request business work. The handler
// observes the derived ctx via r.Context() and downstream helpers that
// respect ctx.Done() will unwind cleanly.
func TimeoutMiddleware(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SafeBodyCloseMiddleware drains and closes r.Body on every exit path
// (success, error, panic — the panic path is recovered by Recovery but
// r.Body would otherwise leak into CLOSE_WAIT). Composes with
// MaxBytesMiddleware; safeBodyClose reads whatever's left after the
// handler pulled what it needed and signals EOF to MaxBytesReader so
// subsequent Close is a no-op drain.
//
// Callers that read from r.Body (httputil.DecodeStrict, etc.) MUST
// still drain any sub-stream they consume; this middleware only
// guards the outer envelope.
func SafeBodyCloseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer func() {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
			}()
		}
		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware catches panics and converts them to 500 errors.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec)
				http.Error(w, "internal error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestIDMiddleware echoes X-Request-ID or generates one and adds it to the response.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r)
	})
}

// APIKeyMiddleware validates X-API-Key against apiKey. Empty apiKey disables auth.
func APIKeyMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey != "" && r.Header.Get("X-API-Key") != apiKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBytesMiddleware caps request bodies locally to protect against OOM DoS.
// Composes with httputil.DecodeStrict which already enforces strict JSON.
func MaxBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SlogMiddleware logs every request after it completes.
func SlogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			slog.Debug("request cancelled", "method", r.Method, "path", r.URL.Path)
			http.Error(w, "request cancelled", 499)
			return
		default:
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
