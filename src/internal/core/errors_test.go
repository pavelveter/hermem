package core_test

import (
	"errors"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// §10 wire-contract unit tests for DomainError.Error() rendering.
//
// The integration tests in src/internal/server/integration_test.go and
// the helper tests in src/internal/httputil/httputil_test.go assert
// strings.Contains checks on the wire envelope — they can MISS a
// regression that drops the inline " (field)" suffix as long as the
// field name still appears somewhere in Error(). The unit tests here
// pin the EXACT rendering format at the source (core.DomainError.Error)
// so a redesign that lifts the field exclusively into the JSON envelope,
// or reorders the suffix, fails immediately.
//
// These are also the §10 contract that DomainError.Error() returns
// "msg (field)" — the inline representation that operator logs and
// grep use to identify the offending input without parsing the
// structured JSON envelope. The structured envelope is the primary
// surface for programmatic clients; the inline rendering is the
// fallback for human/operator paths.

// TestDomainError_RendersMsgWithField pins the §10 with-field format
// exactly: when Field is populated, Error() returns "<Message> (<Field>)".
// Regression-defence against a redesign that drops the parenthetical
// suffix or moves the field exclusively into the JSON attr (still
// §10-valid for code consumers, but operator-log consumers would lose
// the inline diagnostic).
func TestDomainError_RendersMsgWithField(t *testing.T) {
	de := &core.DomainError{
		Message: "unknown field: extra",
		Field:   "extra",
	}
	if got, want := de.Error(), "unknown field: extra (extra)"; got != want {
		t.Fatalf("Error() with field: want %q, got %q", want, got)
	}
}

// TestDomainError_RendersMsgWithoutField pins the no-field format
// exactly: when Field is empty, Error() returns just "<Message>"
// with no parenthetical suffix. Most §10 decode paths (bad_json,
// trailing_data, invalid_type) leave Field="" — clients/learners
// should never see a stray "()" suffix in those paths even if the
// rendering helper accidentally widens the field branch.
func TestDomainError_RendersMsgWithoutField(t *testing.T) {
	de := &core.DomainError{
		Message: "invalid json: unexpected EOF",
	}
	if got, want := de.Error(), "invalid json: unexpected EOF"; got != want {
		t.Fatalf("Error() without field: want %q, got %q", want, got)
	}
}

// TestDomainError_UnwrapReturnsErr pins errors.Is traversability —
// DomainError.Unwrap() returns the sentinel (ErrInvalidInput, etc.) so
// callers can write errors.Is(err, core.ErrInvalidInput) regardless of
// how deep the DomainError is fmt.Errorf-wrapped downstream. This is
// the mechanism that lets §3.2 Wrap + mapStatus route an unwrapped
// error back to the correct HTTP status even after layers of middleware
// have applied their own fmt.Errorf prefixes.
func TestDomainError_UnwrapReturnsErr(t *testing.T) {
	de := core.NewInvalidInputError("bad content")
	if !errors.Is(de, core.ErrInvalidInput) {
		t.Fatal("errors.Is(de, ErrInvalidInput): want true, got false")
	}
	// Same for NotFound so callers can swap the type without rewriting
	// the test — pin the constructor wiring while we're here.
	dnf := core.NewNotFoundError("task abc")
	if !errors.Is(dnf, core.ErrNotFound) {
		t.Fatal("errors.Is(dnf, ErrNotFound): want true, got false")
	}
}
