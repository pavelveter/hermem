// HTTP surface for `hermem view`.
//
// Two routes and nothing else: the embedded page and the snapshot API.
// Kept in this package (not src/internal/server) because the viewer is a
// short-lived loopback process, not part of the shipped REST API.
package view

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"
)

//go:embed index.html
var pageFS embed.FS

// Handler serves the viewer. Every /api/graph request re-reads the DB so
// the page's Refresh button observes concurrent writes.
func Handler(db *sql.DB, limit int) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		// Snapshot is served live; a stale cache would hide new writes.
		w.Header().Set("Cache-Control", "no-store")
		g, err := Snapshot(r.Context(), db, limit)
		if err != nil {
			slog.Error("view: snapshot", "err", err)
			http.Error(w, "snapshot failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := writeJSON(w, g); err != nil {
			slog.Error("view: encode snapshot", "err", err)
		}
	})

	page, err := pageFS.ReadFile("index.html")
	if err != nil {
		// Unreachable: the file is embedded at compile time. Fail loudly
		// rather than serving a blank page if the embed ever breaks.
		panic(fmt.Sprintf("view: embedded page: %v", err))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// "/" is the catch-all pattern, so reject anything else explicitly:
		// the viewer serves exactly two routes.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(page); err != nil {
			slog.Debug("view: write page", "err", err)
		}
	})

	return mux
}

// ServeAndWait binds 127.0.0.1:port, reports the URL via ready, and
// blocks until ctx is cancelled. Loopback-only by construction: the
// snapshot carries raw memory content, so it must not reach the network.
func ServeAndWait(ctx context.Context, db *sql.DB, port string, limit int, ready func(url string)) error {
	if port == "" {
		port = "8421"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("view: invalid port %q: %w", port, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return fmt.Errorf("view: listen on 127.0.0.1:%s: %w", port, err)
	}

	srv := &http.Server{
		Handler:      Handler(db, limit),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	url := "http://" + ln.Addr().String() + "/"
	if ready != nil {
		ready(url)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("view: shutdown", "err", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
