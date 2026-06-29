package api

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "update snapshot files")

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

func TestAllPathsHaveResponses(t *testing.T) {
	spec := GenerateSpec()

	for path, item := range spec.Paths {
		if item.Get != nil && len(item.Get.Responses) == 0 {
			t.Errorf("GET %s has no responses", path)
		}
		if item.Post != nil && len(item.Post.Responses) == 0 {
			t.Errorf("POST %s has no responses", path)
		}
	}
}

func TestUniqueOperationIDs(t *testing.T) {
	spec := GenerateSpec()

	seen := make(map[string]string)
	for path, item := range spec.Paths {
		check := func(op *Operation) {
			if op == nil || op.OperationID == "" {
				return
			}
			if prev, ok := seen[op.OperationID]; ok {
				t.Errorf("duplicate operationId %q: %s and %s", op.OperationID, prev, path)
			}
			seen[op.OperationID] = path
		}
		check(item.Get)
		check(item.Post)
		check(item.Delete)
		check(item.Put)
	}

	expectedIDs := []string{
		"health", "healthLive", "healthReady", "healthStartup", "metrics",
		"store", "search", "retrieve", "query", "queryExplain", "queryTemporal",
		"response", "createEdge", "ingest",
		"taskStatus", "taskExecutable", "taskNext", "taskList", "taskShow",
		"taskDep", "taskTree", "taskCreate", "taskRollback",
		"timeline", "contradictions", "connectedComponents", "communities",
		"graphVerify", "provenance", "recoveryPlan",
		"reEmbed", "dbMigrate", "dbRollback", "dbVerify", "dbSchema",
	}
	for _, id := range expectedIDs {
		if _, ok := seen[id]; !ok {
			t.Errorf("missing expected operationId: %s", id)
		}
	}
}

func TestSpecVersionDeterministic(t *testing.T) {
	BuildVersion = "1.2.3"
	specOnce = sync.Once{}
	cachedSpec = nil

	spec := GenerateSpec()
	if spec.Info.Version != "1.2.3" {
		t.Fatalf("expected version '1.2.3', got %s", spec.Info.Version)
	}

	spec2 := GenerateSpec()
	if spec2.Info.Version != "1.2.3" {
		t.Fatalf("expected cached version '1.2.3', got %s", spec2.Info.Version)
	}
}

func TestSpecBuilderBasic(t *testing.T) {
	spec := NewSpecBuilder().
		Title("Test API").
		Description("A test").
		Version("0.1.0").
		License("MIT").
		Server("http://localhost:9999", "test").
		Tags(Tag{Name: "test"}).
		SecurityScheme("ApiKey", SecurityScheme{Type: "apiKey"}).
		Schemas(map[string]*Schema{"Foo": {Type: "object"}}).
		Paths(map[string]*PathItem{"/foo": {Get: &Operation{OperationID: "foo"}}}).
		Build()

	if spec.Info.Title != "Test API" {
		t.Fatalf("expected title 'Test API', got %s", spec.Info.Title)
	}
	if spec.Info.Version != "0.1.0" {
		t.Fatalf("expected version '0.1.0', got %s", spec.Info.Version)
	}
	if spec.Info.License == nil || spec.Info.License.Name != "MIT" {
		t.Fatal("expected license MIT")
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != "http://localhost:9999" {
		t.Fatal("expected server")
	}
	if len(spec.Tags) != 1 || spec.Tags[0].Name != "test" {
		t.Fatal("expected tag")
	}
	if _, ok := spec.Components.SecuritySchemes["ApiKey"]; !ok {
		t.Fatal("expected security scheme")
	}
	if _, ok := spec.Components.Schemas["Foo"]; !ok {
		t.Fatal("expected schema")
	}
	if _, ok := spec.Paths["/foo"]; !ok {
		t.Fatal("expected path")
	}
}

func TestSnapshotJSON(t *testing.T) {
	specOnce = sync.Once{}
	cachedSpec = nil
	origVersion := BuildVersion
	BuildVersion = "dev"
	defer func() {
		BuildVersion = origVersion
		specOnce = sync.Once{}
		cachedSpec = nil
	}()
	spec := GenerateSpec()
	b := spec.JSON()

	snapshotDir := filepath.Join("testdata")
	snapshotPath := filepath.Join(snapshotDir, "openapi.json")

	if *update {
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snapshotPath, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("snapshot updated:", snapshotPath)
		return
	}

	existing, err := os.ReadFile(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("snapshot file not found; run with -update to create")
		}
		t.Fatal(err)
	}

	var got, want interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(existing, &want); err != nil {
		t.Fatal(err)
	}

	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	wantJSON, _ := json.MarshalIndent(want, "", "  ")

	if string(gotJSON) != string(wantJSON) {
		t.Errorf("spec snapshot mismatch\nexpected length: %d\ngot length: %d\nrun 'go test -update' to refresh", len(wantJSON), len(gotJSON))
	}
}

func TestAllTagsDefined(t *testing.T) {
	spec := GenerateSpec()

	definedTags := make(map[string]bool)
	for _, tag := range spec.Tags {
		definedTags[tag.Name] = true
	}

	for path, item := range spec.Paths {
		check := func(op *Operation) {
			if op == nil {
				return
			}
			for _, tag := range op.Tags {
				if !definedTags[tag] {
					t.Errorf("%s uses undefined tag %q", path, tag)
				}
			}
		}
		check(item.Get)
		check(item.Post)
		check(item.Delete)
		check(item.Put)
	}
}

func TestSchemasNoOrphanRefs(t *testing.T) {
	spec := GenerateSpec()

	schemaNames := make(map[string]bool)
	for name := range spec.Components.Schemas {
		schemaNames[name] = true
	}

	var checkSchema func(s *Schema, path string)
	checkSchema = func(s *Schema, path string) {
		if s == nil {
			return
		}
		if s.Ref != "" {
			name := s.Ref[len("#/components/schemas/"):]
			if !schemaNames[name] {
				t.Errorf("%s references undefined schema %q", path, name)
			}
		}
		for k, v := range s.Properties {
			checkSchema(v, path+".properties."+k)
		}
		if s.Items != nil {
			checkSchema(s.Items, path+".items")
		}
	}

	for name, s := range spec.Components.Schemas {
		checkSchema(s, "schemas."+name)
	}

	for path, item := range spec.Paths {
		if item.Get != nil {
			if item.Get.RequestBody != nil {
				for ct, mt := range item.Get.RequestBody.Content {
					checkSchema(mt.Schema, path+".GET.requestBody.content."+ct)
				}
			}
			for code, resp := range item.Get.Responses {
				for ct, mt := range resp.Content {
					checkSchema(mt.Schema, path+".GET.responses."+code+".content."+ct)
				}
			}
		}
		if item.Post != nil {
			if item.Post.RequestBody != nil {
				for ct, mt := range item.Post.RequestBody.Content {
					checkSchema(mt.Schema, path+".POST.requestBody.content."+ct)
				}
			}
			for code, resp := range item.Post.Responses {
				for ct, mt := range resp.Content {
					checkSchema(mt.Schema, path+".POST.responses."+code+".content."+ct)
				}
			}
		}
	}
}

func TestAllSchemasListed(t *testing.T) {
	schemas := AllSchemas()

	expected := []string{
		"Entity", "Edge", "ErrorResponse", "StoreRequest", "SearchRequest",
		"RetrieveRequest", "IngestRequest", "EdgeRequest", "SearchResult",
		"RetrievalResult", "RetrievedFact", "GraphNode", "ScoreBreakdown",
		"TaskStatusRequest", "TaskListRequest", "TaskShowRequest", "TaskShowResponse",
		"TaskDepRequest", "TaskCreateRequest", "TaskCreateResponse",
		"TaskRollbackRequest", "TaskRollbackResponse", "TaskTreeRequest",
		"TaskTreeResponse", "TaskExecutableResponse", "ContradictionPair",
		"ConnectedComponent", "Community", "VerifyReport",
		"ReEmbedRequest", "ReEmbedResult", "HealthResponse", "ReadyResponse",
		"TimelineEntry", "MigStatus", "SchemaReport", "QueryResponse",
		"ResponseResult", "Stats", "IntegrityReport", "IntegrityIssue",
	}

	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range expected {
		if _, ok := schemas[name]; !ok {
			t.Errorf("AllSchemas missing: %s", name)
		}
	}

	_ = names
}

func TestAllPathsListed(t *testing.T) {
	paths := AllPaths()

	expected := []string{
		"/health", "/health/live", "/health/ready", "/health/startup",
		"/metrics",
		"/store", "/search", "/retrieve", "/query", "/query/explain",
		"/query/temporal", "/response", "/edge", "/ingest",
		"/task/status", "/task/executable", "/task/next", "/task/list",
		"/task/show", "/task/dep", "/task/tree", "/task/create",
		"/task/rollback",
		"/timeline", "/contradictions", "/connected-components", "/communities",
		"/graph/verify", "/provenance", "/recovery-plan",
		"/admin/re-embed",
		"/db/migrate", "/db/rollback", "/db/verify", "/db/schema",
	}

	for _, p := range expected {
		if _, ok := paths[p]; !ok {
			t.Errorf("AllPaths missing: %s", p)
		}
	}
}
