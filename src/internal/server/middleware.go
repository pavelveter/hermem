package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"

	"github.com/pavelveter/hermem/src/internal/auth"
)

func TimeoutMiddleware(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

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

func AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env := GetRuntime(r.Context())
			if env == nil || env.Cfg == nil {
				writeAuthError(w, http.StatusInternalServerError, "internal_error")
				return
			}

			if !authEnabled(env.Cfg) {
				next.ServeHTTP(w, r)
				return
			}

			path := strings.TrimPrefix(r.URL.Path, "/")
			if strings.HasPrefix(path, "health") {
				next.ServeHTTP(w, r)
				return
			}

			keys := buildKeysFromCfg(env.Cfg)
			authenticator := auth.NewStaticAuthenticator(keys)

			raw := r.Header.Get("X-API-Key")
			required := auth.ScopeForPath(path)

			_, ok, err := authenticator.Authorize(raw, required)
			if errors.Is(err, auth.ErrInsufficientScope) {
				writeAuthError(w, http.StatusForbidden, "insufficient_scope")
				return
			}
			if errors.Is(err, auth.ErrInvalidKey) || !ok {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func authEnabled(cfg *config.Config) bool {
	return cfg.APIKey != "" || len(cfg.APIKeys) > 0
}

func buildKeysFromCfg(cfg *config.Config) []auth.Key {
	if len(cfg.APIKeys) > 0 {
		return cfg.APIKeys
	}
	if cfg.APIKey != "" {
		return []auth.Key{{Value: cfg.APIKey, Scope: auth.ScopeAdmin}}
	}
	return nil
}

func writeAuthError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}

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
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		reqID := r.Header.Get("X-Request-ID")
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", time.Since(start),
			"request_id", reqID,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

type envKey struct{}

func RuntimeMiddleware(mgr *clienv.EnvManager, logger *slog.Logger) func(http.Handler) http.Handler {
	if mgr == nil {
		panic("server: RuntimeMiddleware called with nil EnvManager (config wiring bug)")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			snapshot := mgr.Get()
			if snapshot == nil {
				logger.Error("runtime middleware: EnvManager empty — request rejected",
					"method", r.Method, "path", r.URL.Path)
				http.Error(w, "Internal Server Error: Runtime Not Initialized", http.StatusInternalServerError)
				return
			}
			ctx := context.WithValue(r.Context(), envKey{}, snapshot)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetRuntime(ctx context.Context) *clienv.Env {
	if e, ok := ctx.Value(envKey{}).(*clienv.Env); ok {
		return e
	}
	return nil
}

// APIVersionMiddleware sets the X-Hermem-API-Version response header
// on every response. The value is the server's MAJOR.MINOR version
// (e.g. "0.3.0"). SDKs use this for SemVer negotiation: if the
// server's MAJOR differs from the SDK's MAJOR, the SDK should warn
// or fail.
func APIVersionMiddleware(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Hermem-API-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}
