package api

import (
	"encoding/json"
	"testing"
)

func TestGenerateSpec(t *testing.T) {
	spec := GenerateSpec()

	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("expected openapi 3.1.0, got %s", spec.OpenAPI)
	}
	if spec.Info.Title != "Hermem API" {
		t.Fatalf("expected title 'Hermem API', got %s", spec.Info.Title)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("expected paths to be non-empty")
	}
	if len(spec.Components.Schemas) == 0 {
		t.Fatal("expected schemas to be non-empty")
	}
}

func TestSpecJSON(t *testing.T) {
	spec := GenerateSpec()
	b := spec.JSON()

	var parsed map[string]interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths, ok := parsed["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		t.Fatal("paths missing or empty in JSON output")
	}
}

func TestSpecYAML(t *testing.T) {
	spec := GenerateSpec()
	b := spec.YAMLBytes()
	if len(b) == 0 {
		t.Fatal("YAML output is empty")
	}
}

func TestPathsCoverAllEndpoints(t *testing.T) {
	spec := GenerateSpec()

	expectedPaths := []string{
		"/health", "/health/live", "/health/ready", "/health/startup",
		"/metrics",
		"/store", "/search", "/retrieve", "/query", "/query/explain", "/query/temporal",
		"/response", "/edge", "/ingest",
		"/task/status", "/task/executable", "/task/next", "/task/list", "/task/show",
		"/task/dep", "/task/tree", "/task/create", "/task/rollback",
		"/timeline", "/contradictions", "/connected-components", "/communities",
		"/graph/verify", "/provenance", "/recovery-plan",
		"/admin/re-embed",
		"/db/migrate", "/db/rollback", "/db/verify", "/db/schema",
	}

	for _, p := range expectedPaths {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("missing path: %s", p)
		}
	}
}

func TestSchemasHaveRequiredFields(t *testing.T) {
	spec := GenerateSpec()

	required := []string{
		"ErrorResponse", "StoreRequest", "SearchRequest", "RetrieveRequest",
		"IngestRequest", "EdgeRequest", "Entity", "Edge",
	}
	for _, name := range required {
		if _, ok := spec.Components.Schemas[name]; !ok {
			t.Errorf("missing schema: %s", name)
		}
	}
}

func TestAllPathsHaveOperationID(t *testing.T) {
	spec := GenerateSpec()

	for path, item := range spec.Paths {
		if item.Get != nil && item.Get.OperationID == "" {
			t.Errorf("GET %s missing operationId", path)
		}
		if item.Post != nil && item.Post.OperationID == "" {
			t.Errorf("POST %s missing operationId", path)
		}
	}
}

func TestAllPathsHaveTags(t *testing.T) {
	spec := GenerateSpec()

	for path, item := range spec.Paths {
		if item.Get != nil && len(item.Get.Tags) == 0 {
			t.Errorf("GET %s missing tags", path)
		}
		if item.Post != nil && len(item.Post.Tags) == 0 {
			t.Errorf("POST %s missing tags", path)
		}
	}
}
