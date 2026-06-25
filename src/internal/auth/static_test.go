package auth_test

import (
	"errors"
	"testing"

	"github.com/pavelveter/hermem/src/internal/auth"
)

func TestStaticAuthenticator_EmptyRaw(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "secret", Scope: auth.ScopeAdmin},
	})
	_, ok, err := a.Authorize("", auth.ScopeRead)
	if ok {
		t.Fatal("expected false for empty raw")
	}
	if !errors.Is(err, auth.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestStaticAuthenticator_ValidKeyCorrectScope(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "key-read", Scope: auth.ScopeRead},
	})
	key, ok, err := a.Authorize("key-read", auth.ScopeRead)
	if !ok || err != nil {
		t.Fatalf("expected ok=true, err=nil; got ok=%v, err=%v", ok, err)
	}
	if key.Value != "key-read" {
		t.Fatalf("expected returned key value 'key-read', got %q", key.Value)
	}
}

func TestStaticAuthenticator_InsufficientScope(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "key-read", Scope: auth.ScopeRead},
	})
	_, ok, err := a.Authorize("key-read", auth.ScopeAdmin)
	if ok {
		t.Fatal("expected false for insufficient scope")
	}
	if !errors.Is(err, auth.ErrInsufficientScope) {
		t.Fatalf("expected ErrInsufficientScope, got %v", err)
	}
}

func TestStaticAuthenticator_AdminCanAccessRead(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "key-admin", Scope: auth.ScopeAdmin},
	})
	_, ok, err := a.Authorize("key-admin", auth.ScopeRead)
	if !ok || err != nil {
		t.Fatalf("expected ok=true (admin can read), got ok=%v, err=%v", ok, err)
	}
}

func TestStaticAuthenticator_WriteCanAccessRead(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "key-write", Scope: auth.ScopeWrite},
	})
	_, ok, err := a.Authorize("key-write", auth.ScopeRead)
	if !ok || err != nil {
		t.Fatalf("expected ok=true (write can read), got ok=%v, err=%v", ok, err)
	}
}

func TestStaticAuthenticator_MultipleKeysOnlyOneMatches(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "key-a", Scope: auth.ScopeRead},
		{Value: "key-b", Scope: auth.ScopeWrite},
	})
	_, ok, err := a.Authorize("key-a", auth.ScopeRead)
	if !ok || err != nil {
		t.Fatalf("key-a should match read scope: ok=%v, err=%v", ok, err)
	}
	_, ok, err = a.Authorize("key-b", auth.ScopeWrite)
	if !ok || err != nil {
		t.Fatalf("key-b should match write scope: ok=%v, err=%v", ok, err)
	}
}

func TestStaticAuthenticator_UnknownKey(t *testing.T) {
	a := auth.NewStaticAuthenticator([]auth.Key{
		{Value: "secret", Scope: auth.ScopeAdmin},
	})
	_, ok, err := a.Authorize("wrong", auth.ScopeRead)
	if ok {
		t.Fatal("expected false for unknown key")
	}
	if !errors.Is(err, auth.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}
