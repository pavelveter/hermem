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
