package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey string

const reqIDKey ctxKey = "request_id"

func withReqID(ctx context.Context, attrs ...any) []any {
	if ctx == nil {
		return attrs
	}
	if id := getReqID(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	return attrs
}

func getReqID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(reqIDKey).(string); ok {
		return id
	}
	return ""
}

func generateReqID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateReqID()
		}
		ctx := context.WithValue(r.Context(), reqIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(apiKey)) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"event", "panic_recovery",
					"path", r.URL.Path,
					"method", r.Method,
					"panic", fmt.Sprintf("%v", rec),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := getReqID(r.Context())
		slog.Debug("request started",
			"event", "http_request_start",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", reqID,
		)
		next.ServeHTTP(w, r)
		slog.Debug("request completed",
			"event", "http_request_end",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", reqID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
