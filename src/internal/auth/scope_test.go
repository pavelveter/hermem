package auth_test

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/auth"
)

func TestScope_CanAccess(t *testing.T) {
	tests := []struct {
		have     auth.Scope
		require  auth.Scope
		expectOK bool
	}{
		{auth.ScopeAdmin, auth.ScopeAdmin, true},
		{auth.ScopeAdmin, auth.ScopeWrite, true},
		{auth.ScopeAdmin, auth.ScopeRead, true},
		{auth.ScopeWrite, auth.ScopeWrite, true},
		{auth.ScopeWrite, auth.ScopeRead, true},
		{auth.ScopeWrite, auth.ScopeAdmin, false},
		{auth.ScopeRead, auth.ScopeRead, true},
		{auth.ScopeRead, auth.ScopeWrite, false},
		{auth.ScopeRead, auth.ScopeAdmin, false},
		{auth.Scope(""), auth.ScopeRead, false},
		{auth.Scope(""), auth.ScopeWrite, false},
		{auth.Scope(""), auth.ScopeAdmin, false},
	}
	for _, tt := range tests {
		got := tt.have.CanAccess(tt.require)
		if got != tt.expectOK {
			t.Errorf("Scope(%q).CanAccess(%q) = %v, want %v", tt.have, tt.require, got, tt.expectOK)
		}
	}
}

func TestScopeForPath(t *testing.T) {
	tests := []struct {
		path string
		want auth.Scope
	}{
		{"ingest", auth.ScopeWrite},
		{"ingest/something", auth.ScopeWrite},
		{"search", auth.ScopeRead},
		{"search?q=test", auth.ScopeRead},
		{"retrieve", auth.ScopeRead},
		{"query", auth.ScopeRead},
		{"admin", auth.ScopeAdmin},
		{"admin/stats", auth.ScopeAdmin},
		{"unknown", auth.ScopeWrite},
		{"", auth.ScopeWrite},
	}
	for _, tt := range tests {
		got := auth.ScopeForPath(tt.path)
		if got != tt.want {
			t.Errorf("ScopeForPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestRequiredScopesMap(t *testing.T) {
	m := auth.RequiredScopesMap()
	if len(m) != 5 {
		t.Errorf("RequiredScopesMap() returned %d entries, want 5", len(m))
	}
	m["custom"] = auth.ScopeAdmin
	m2 := auth.RequiredScopesMap()
	if _, ok := m2["custom"]; ok {
		t.Error("RequiredScopesMap() returned a reference to the internal map, want a copy")
	}
}

func TestParseScope(t *testing.T) {
	tests := []struct {
		s    string
		want auth.Scope
	}{
		{"read", auth.ScopeRead},
		{"write", auth.ScopeWrite},
		{"admin", auth.ScopeAdmin},
		{"unknown", auth.Scope("")},
		{"READ", auth.Scope("")},
		{"", auth.Scope("")},
	}
	for _, tt := range tests {
		got := auth.ParseScope(tt.s)
		if got != tt.want {
			t.Errorf("ParseScope(%q) = %q, want %q", tt.s, got, tt.want)
		}
	}
}
