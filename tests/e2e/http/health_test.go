package http

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestHealthEndpoint(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/health")
	helpers.MustStatus(t, resp, 200)
	m := helpers.MustJSONMap(t, resp)
	helpers.AssertJSONField(t, m, "status", "ok")
}

func TestHealthLiveEndpoint(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/health/live")
	helpers.MustStatus(t, resp, 200)
}

func TestHealthReadyEndpoint(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/health/ready")
	helpers.MustStatus(t, resp, 200)
}

func TestMetricsEndpoint(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/metrics")
	helpers.MustStatus(t, resp, 200)
}
