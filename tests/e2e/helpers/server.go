package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// FindFreePort returns an available TCP port.
func FindFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	ta, ok := l.Addr().(*net.TCPAddr)
	_ = l.Close()
	if !ok {
		t.Fatal("unexpected address type")
	}
	return ta.Port
}

// Server represents a running hermem server for testing.
type Server struct {
	Port   int
	URL    string
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// StartServer starts hermem serve in background.
func StartServer(t *testing.T, dir string) *Server {
	t.Helper()
	port := FindFreePort(t)
	binary := BinaryPath(t)

	// Place hermem.ini next to the binary (hermem looks for it relative to the executable)
	binDir := filepath.Dir(binary)
	iniPath := filepath.Join(binDir, "hermem.ini")
	dbPath := filepath.Join(dir, "hermem.db")

	// Use pre-written config from dir if it exists, otherwise create minimal config
	dirConfig := filepath.Join(dir, "hermem.ini")
	var iniContent string
	if data, err := os.ReadFile(dirConfig); err == nil {
		iniContent = string(data)
	} else {
		iniContent = fmt.Sprintf("[database]\npath = %s\nbackend = in-memory\nauto_migrate = true\n", dbPath)
	}
	if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(iniPath) })

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "serve", "--port", fmt.Sprintf("%d", port), "--skip-embedder-check")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir)

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}

	srv := &Server{
		Port:   port,
		URL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		cmd:    cmd,
		cancel: cancel,
	}

	t.Cleanup(func() {
		cancel()
		srv.cmd.Process.Wait() //nolint:errcheck
	})

	// Wait for server to be ready (use /health/startup which always returns 200)
	WaitForHealth(t, srv.URL+"/health/startup", 10*time.Second)
	return srv
}

// SkipIfNoEmbedder skips the test if the Ollama embedder is not reachable.
func SkipIfNoEmbedder(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		t.Skip("Ollama not available — skipping embedder-dependent test")
	}
	resp.Body.Close()
}
func WaitForHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server not ready after %v", timeout)
}
