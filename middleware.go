package main

import (
	"context"
	"crypto/rand"
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
			if r.Header.Get("X-API-Key") != apiKey {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
