package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var extCtx = context.Background()

// entityIDs returns just the IDs from a slice, for terse assertion messages.
func entityIDs(es []ExtractedEntity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func TestFilterEntitiesDropsInvalidCategory(t *testing.T) {
	in := []ExtractedEntity{
		{ID: "ok-world", Category: "world", Content: "x"},
		{ID: "bad-extra", Category: "extraterrestrial", Content: "y"},
		{ID: "ok-opinion", Category: "opinion", Content: "z"},
		{ID: "bad-empty", Category: "", Content: ""},
	}
	got := filterEntities(in)
	want := []string{"ok-world", "ok-opinion"}
	gotIDs := entityIDs(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("got %d entities (%v), want %d (%v)", len(gotIDs), gotIDs, len(want), want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Errorf("position %d: id = %q, want %q", i, gotIDs[i], want[i])
		}
	}
}

func TestFilterRelationsDropsInvalidTypeAndEmptyTarget(t *testing.T) {
	in := []Relation{
		{TargetID: "x", RelationType: "related_to"},
		{TargetID: "y", RelationType: "thinks_about"}, // invalid type
		{TargetID: "", RelationType: "prefers"},       // empty target
		{TargetID: "z", RelationType: "prefers"},
	}
	out := filterRelations(in)
	if len(out) != 2 {
		t.Fatalf("got %d relations, want 2", len(out))
	}
	if out[0].TargetID != "x" || out[1].TargetID != "z" {
		t.Errorf("survivors = %+v, want [{x,related_to} {z,prefers}]", out)
	}
}

// TestExtractEntitiesHappy verifies the 200 + valid JSON path.
func TestExtractEntitiesHappy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatResponse{
			Message: chatMessage{
				Role: "assistant",
				Content: `{"entities":[{"id":"a","category":"world","content":"A"},` +
					`{"id":"b","category":"opinion","content":"B",` +
					`"relations":[{"target_id":"a","relation_type":"related_to"}]}]}`,
			},
			Done: true,
		})
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "test-model", 0.1, 0)
	res, err := 	ex.ExtractEntities(extCtx, "user-dialog")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if len(res.Entities) != 2 {
		t.Fatalf("got %d entities, want 2", len(res.Entities))
	}
	if res.Entities[0].ID != "a" {
		t.Errorf("first entity id = %q, want a", res.Entities[0].ID)
	}
	if len(res.Entities[1].Relations) != 1 {
		t.Errorf("second entity should have 1 relation, got %d", len(res.Entities[1].Relations))
	}
	if res.Entities[1].Relations[0].RelationType != "related_to" {
		t.Errorf("second entity relation type = %q, want related_to", res.Entities[1].Relations[0].RelationType)
	}
}

// TestExtractEntitiesEmptyContentReturnsEmpty verifies that an Ollama
// response with empty content (e.g. the model "answered nothing") is
// reported as a successful empty result, NOT a parse error.
func TestExtractEntitiesEmptyContentReturnsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatResponse{Message: chatMessage{Content: ""}, Done: true})
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	res, err := 	ex.ExtractEntities(extCtx, "d")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if len(res.Entities) != 0 {
		t.Errorf("entities = %d, want 0", len(res.Entities))
	}
}

// TestExtractEntitiesParseErrorNoRetry verifies that JSON parse errors
// return immediately (no retry): the API returned 200, so retrying
// gets the same broken body. Only network errors and 5xx retry.
func TestExtractEntitiesParseErrorNoRetry(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-valid-json{")
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	if _, err := 	ex.ExtractEntities(extCtx, "d"); err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server hit %d times for parse failure, want 1 (no retry on JSON error)", got)
	}
}

// TestExtractEntitiesRetriesOn5xx verifies retry-on-5xx: 2 transient
// 500s then a 200 → expects success. The HTTP server is in-memory so
// the second call completes in microseconds; backoff 200+400ms ≈ 600ms
// overall. We give a generous 5s budget just in case.
func TestExtractEntitiesRetriesOn5xx(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "transient")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatResponse{
			Message: chatMessage{Role: "assistant", Content: `{"entities":[]}`},
			Done:    true,
		})
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	start := time.Now()
	res, err := 	ex.ExtractEntities(extCtx, "d")
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("retry path took %v, want < 5s", elapsed)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server hit %d times, want 3 (2 fails + 1 success)", got)
	}
	if len(res.Entities) != 0 {
		t.Errorf("entities = %d, want 0 (empty payload)", len(res.Entities))
	}
}

// TestExtractEntitiesAllRetriesFail verifies that 3 consecutive 500s
// surface as an "after N attempts" error, not a hard panic.
func TestExtractEntitiesAllRetriesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	_, err := 	ex.ExtractEntities(extCtx, "d")
	if err == nil {
		t.Fatal("expected exhausted-retry error, got nil")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Errorf("error didn't mention attempts: %v", err)
	}
}

// TestExtractEntitiesNonRetryHTTP4xx verifies 4xx errors (other than
// 200/5xx) return immediately — they indicate client mistakes that
// retrying won't fix.
func TestExtractEntitiesNonRetryHTTP4xx(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad model name")
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	_, err := 	ex.ExtractEntities(extCtx, "d")
	if err == nil {
		t.Fatal("expected 4xx error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server hit %d times for 4xx, want 1 (no retry on client errors)", got)
	}
}

// TestStripMarkdownCodeFence enumerates the contract of the JSON-fence
// stripper called by both Ollama and OpenAI ExtractEntities. Each case
// is a representative LLM response (fenced / bare / wrapped / unclosed).
func TestStripMarkdownCodeFence(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare json unchanged", `{"entities":[{"id":"a","category":"world","content":"A"}]}`, `{"entities":[{"id":"a","category":"world","content":"A"}]}`},
		{"json fence stripped", "```json\n{\"entities\":[]}\n```", `{"entities":[]}`},
		{"generic fence stripped", "```\n{\"entities\":[]}\n```", `{"entities":[]}`},
		{"uppercase JSON tag", "```JSON\n{\"entities\":[]}\n```", `{"entities":[]}`},
		{"trailing whitespace", "```json\n{\"entities\":[]}\n``` \t", `{"entities":[]}`},
		{"leading whitespace only", "   \n\r{\"entities\":[]}", `{"entities":[]}`},
		{"preamble and postamble", "Here you go:\n```json\n{\"entities\":[]}\n``` done.", `{"entities":[]}`},
		{"unclosed fence best effort", "```json\n{\"entities\":[]}", `{"entities":[]}`},
		{"empty input", "", ""},
		{"only fence chars", "```\n```", ""},
		{"nested-looking fence takes first only", "prefix ```json{a}``` suffix ```json{b}```", "{a}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripMarkdownCodeFence(tc.input)
			if got != tc.want {
				t.Errorf("stripMarkdownCodeFence(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestExtractEntitiesStripsMarkdownFences is an integration test
// through the public ExtractEntities path: a server that emits
// ```json ... ``` wrapped JSON must still parse successfully instead
// of returning a parse error.
func TestExtractEntitiesStripsMarkdownFences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatResponse{
			Message: chatMessage{
				Role: "assistant",
				Content: "```json\n" +
					`{"entities":[{"id":"fenced","category":"world","content":"Fenced entity"}]}` +
					"\n```",
			},
			Done: true,
		})
	}))
	defer server.Close()

	ex := NewOllamaLLMExtractor(server.URL, "m", 0.1, 0)
	res, err := ex.ExtractEntities(extCtx, "d")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if len(res.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(res.Entities))
	}
	if res.Entities[0].ID != "fenced" {
		t.Errorf("entity id = %q, want fenced", res.Entities[0].ID)
	}
}
