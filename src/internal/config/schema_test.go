package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/ini.v1"

	"github.com/pavelveter/hermem/src/internal/core"
)

// --- ValidateSchema ---

func TestValidateSchema_DefaultIsValid(t *testing.T) {
	if err := ValidateSchema(core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("default schema must pass: %v", err)
	}
}

func TestValidateSchema_EmptyCategories(t *testing.T) {
	s := core.DefaultSchemaConfig(false)
	s.AllowedCategories = nil
	err := ValidateSchema(s)
	if err == nil || !strings.Contains(err.Error(), "allowed_categories") {
		t.Fatalf("want allowed_categories error, got %v", err)
	}
}

func TestValidateSchema_EmptyRelations(t *testing.T) {
	s := core.DefaultSchemaConfig(false)
	s.AllowedRelations = nil
	err := ValidateSchema(s)
	if err == nil || !strings.Contains(err.Error(), "allowed_relations") {
		t.Fatalf("want allowed_relations error, got %v", err)
	}
}

func TestValidateSchema_DuplicateStateInOrder(t *testing.T) {
	s := core.DefaultSchemaConfig(true)
	s.ValidStateOrder = []string{"pending", "pending", "done"}
	s.ValidStates = map[string]bool{"pending": true, "done": true}
	err := ValidateSchema(s)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate state error, got %v", err)
	}
}

func TestValidateSchema_StatefulWithoutStates(t *testing.T) {
	s := core.DefaultSchemaConfig(true)
	s.StatefulCategories = map[string]bool{"task": true}
	s.ValidStateOrder = nil
	s.ValidStates = nil
	err := ValidateSchema(s)
	if err == nil || !strings.Contains(err.Error(), "valid_states") {
		t.Fatalf("want valid_states error, got %v", err)
	}
}

func TestValidateSchema_StateUnblockingNotInValidStates(t *testing.T) {
	s := core.DefaultSchemaConfig(true)
	// DefaultSchemaConfig(true) leaves ValidStates empty, so populate it so
	// the validate path actually checks StateUnblocking membership.
	s.ValidStateOrder = []string{"pending", "done"}
	s.ValidStates = map[string]bool{"pending": true, "done": true}
	s.StateUnblocking = "rolled_back" // not in {pending, done}
	if err := ValidateSchema(s); err == nil {
		t.Fatal("expected error when state_unblocking is not in valid_states")
	}
}

func TestValidateSchema_RelationBlockingNotInAllowedRelations(t *testing.T) {
	s := core.DefaultSchemaConfig(false)
	s.RelationBlocking = "mystery_rel"
	if err := ValidateSchema(s); err == nil {
		t.Fatal("expected error when relation_blocking is not in allowed_relations")
	}
}

func TestValidateSchema_RelationRecoveryNotInAllowedRelations(t *testing.T) {
	s := core.DefaultSchemaConfig(false)
	s.RelationRecovery = "mystery_rel"
	if err := ValidateSchema(s); err == nil {
		t.Fatal("expected error when relation_recovery is not in allowed_relations")
	}
}

func TestValidateSchema_StateUnblockingEmptyOK(t *testing.T) {
	s := core.DefaultSchemaConfig(true)
	s.StateUnblocking = ""
	if err := ValidateSchema(s); err != nil {
		t.Fatalf("empty state_unblocking should pass: %v", err)
	}
}

// --- ParseSchemaSection ---

func writeINI(t *testing.T, content string) (string, *ini.File) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write ini: %v", err)
	}
	f, err := ini.Load(path)
	if err != nil {
		t.Fatalf("ini.Load: %v", err)
	}
	return path, f
}

func minimalINI(extra string) string {
	return `[schema]
allowed_categories = world, opinion
allowed_relations = uses, related_to
` + extra
}

func TestParseSchemaSection_MinimalValid(t *testing.T) {
	path, f := writeINI(t, minimalINI(""))
	s, err := ParseSchemaSection(f.Section("schema"), path)
	if err != nil {
		t.Fatalf("parse minimal: %v", err)
	}
	if !s.AllowedCategories["world"] || !s.AllowedCategories["opinion"] {
		t.Fatal("missing categories")
	}
	if !s.AllowedRelations["uses"] {
		t.Fatal("missing relations")
	}
}

func TestParseSchemaSection_UnknownKey(t *testing.T) {
	ini := minimalINI("mystery_key = 42\n")
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want unknown key error, got %v", err)
	}
}

func TestParseSchemaSection_NameKeyIsAllowed(t *testing.T) {
	// `name` in the [schema] section header is allowed by the allowlist.
	ini := `[schema]
name = custom
allowed_categories = world
allowed_relations = uses
`
	path, f := writeINI(t, ini)
	if _, err := ParseSchemaSection(f.Section("schema"), path); err != nil {
		t.Fatalf("name key should be tolerated: %v", err)
	}
}

func TestParseSchemaSection_MissingAllowedCategories(t *testing.T) {
	ini := `[schema]
allowed_relations = uses
`
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "allowed_categories") {
		t.Fatalf("want allowed_categories error, got %v", err)
	}
}

func TestParseSchemaSection_MissingAllowedRelations(t *testing.T) {
	ini := `[schema]
allowed_categories = world
`
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "allowed_relations") {
		t.Fatalf("want allowed_relations error, got %v", err)
	}
}

func TestParseSchemaSection_StatefulRequiresValidStates(t *testing.T) {
	ini := `[schema]
allowed_categories = world, task
allowed_relations = uses, blocked_by
stateful_categories = task
`
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "valid_states") {
		t.Fatalf("want valid_states error, got %v", err)
	}
}

func TestParseSchemaSection_StatefulCategoryMustBeAllowed(t *testing.T) {
	ini := `[schema]
allowed_categories = world
allowed_relations = uses, blocked_by
stateful_categories = task
valid_states = pending, done
`
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "not in allowed_categories") {
		t.Fatalf("want not-in-allowed-categories error, got %v", err)
	}
}

func TestParseSchemaSection_StateUnblockingNotInValidStates(t *testing.T) {
	ini := `[schema]
allowed_categories = world, task
allowed_relations = uses, blocked_by, recovers_via
stateful_categories = task
valid_states = pending, done
relation_blocking = blocked_by
relation_recovery = recovers_via
state_unblocking = rolled_back
`
	path, f := writeINI(t, ini)
	_, err := ParseSchemaSection(f.Section("schema"), path)
	if err == nil || !strings.Contains(err.Error(), "state_unblocking") {
		t.Fatalf("want state_unblocking error, got %v", err)
	}
}

func TestParseSchemaSection_FullValid(t *testing.T) {
	ini := `[schema]
allowed_categories = world, opinion, task
allowed_relations = uses, blocked_by, recovers_via
stateful_categories = task
valid_states = pending, in_progress, done
relation_blocking = blocked_by
relation_recovery = recovers_via
state_unblocking = done
`
	path, f := writeINI(t, ini)
	s, err := ParseSchemaSection(f.Section("schema"), path)
	if err != nil {
		t.Fatalf("parse full: %v", err)
	}
	if s.RelationBlocking != "blocked_by" || s.RelationRecovery != "recovers_via" {
		t.Fatalf("relations: %+v", s)
	}
	if s.StateUnblocking != "done" {
		t.Fatalf("state_unblocking: %q", s.StateUnblocking)
	}
	if len(s.ValidStateOrder) != 3 {
		t.Fatalf("valid_states order: want 3, got %v", s.ValidStateOrder)
	}
}
