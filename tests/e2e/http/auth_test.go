package http

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestAuthenticationRequired(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir))+"\n[server]\napi_key = test-secret-key\n")
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Request to protected endpoint without API key should fail
	resp := client.Get(t, "/timeline")
	helpers.MustStatus(t, resp, 401)
}

func TestAuthenticationValidKey(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir))+"\n[server]\napi_key = test-secret-key\n")
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL).WithAuth("test-secret-key")

	resp := client.Get(t, "/timeline")
	helpers.MustStatus(t, resp, 200)
}

func TestAuthenticationInvalidKey(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir))+"\n[server]\napi_key = test-secret-key\n")
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL).WithAuth("wrong-key")

	resp := client.Get(t, "/timeline")
	helpers.MustStatus(t, resp, 401)
}

func TestAuthenticationDisabled(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// No API key configured - should work
	resp := client.Get(t, "/health")
	helpers.MustStatus(t, resp, 200)
}
