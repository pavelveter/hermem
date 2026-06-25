package admin

import (
	"os"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/config"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(key) != 64 {
		t.Fatalf("want 64 hex chars, got %d: %q", len(key), key)
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("non-hex char %c in key %q", c, key)
		}
	}
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		k, _ := GenerateKey()
		if seen[k] {
			t.Fatal("collision at iteration", i)
		}
		seen[k] = true
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc", "abc"},
		{"abcdefgh", "abcdefgh"},
		{"abcdefghij", "abcd...ghij"},
		{"0123456789abcdef", "0123...cdef"},
		{"", ""},
	}
	for _, tt := range tests {
		got := MaskKey(tt.input)
		if got != tt.want {
			t.Errorf("MaskKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseKeySpec(t *testing.T) {
	t.Run("key only", func(t *testing.T) {
		k := config.ParseKeySpec("mykey")
		if k == nil {
			t.Fatal("expected non-nil key")
		}
		if k.Value != "mykey" || string(k.Scope) != "admin" || k.Label != "" {
			t.Fatalf("got value=%q scope=%q label=%q", k.Value, k.Scope, k.Label)
		}
	})
	t.Run("key:scope", func(t *testing.T) {
		k := config.ParseKeySpec("mykey:read")
		if k.Value != "mykey" || string(k.Scope) != "read" || k.Label != "" {
			t.Fatalf("got value=%q scope=%q label=%q", k.Value, k.Scope, k.Label)
		}
	})
	t.Run("key:scope:label", func(t *testing.T) {
		k := config.ParseKeySpec("mykey:write:ci-bot")
		if k.Value != "mykey" || string(k.Scope) != "write" || k.Label != "ci-bot" {
			t.Fatalf("got value=%q scope=%q label=%q", k.Value, k.Scope, k.Label)
		}
	})
	t.Run("empty string", func(t *testing.T) {
		k := config.ParseKeySpec("")
		if k != nil {
			t.Fatal("expected nil for empty string")
		}
	})
}

func TestWriteIniEntry(t *testing.T) {
	path := writeTestIni(t, "")
	defer os.Remove(path)

	key := "abcdef1234567890abcdef1234567890"
	if err := config.AddKeyToFile(path, key, "read", "test-key"); err != nil {
		t.Fatalf("AddKeyToFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, key) {
		t.Fatalf("added key not found in %q", content)
	}
	if !strings.Contains(content, "test-key") {
		t.Fatalf("label not found in %q", content)
	}
	if !strings.Contains(content, "read") {
		t.Fatalf("scope not found in %q", content)
	}
}

func TestRevokeKey(t *testing.T) {
	path := writeTestIni(t, "api_keys = oldkey:read:test-label")
	defer os.Remove(path)

	if err := config.RemoveKeyFromFile(path, "test-label"); err != nil {
		t.Fatalf("RemoveKeyFromFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "oldkey") {
		t.Fatalf("revoked key still present in %q", content)
	}
}

func writeTestIni(t *testing.T, apiKeysLine string) string {
	t.Helper()
	f, err := os.CreateTemp("", "hermem-test-*.ini")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	content := "[server]\n"
	if apiKeysLine != "" {
		content += apiKeysLine + "\n"
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()
	return f.Name()
}
