// Package apperr owns the legacy application error contract:
// a typed code + message (+ optional field) rendered as "msg (field)".
//
// This is a verbatim relocation of the former core.DomainError — the
// §10 wire contract (HTTP envelopes and CLI stderr render Error()
// byte-exactly) is preserved. Adoption of the pkg/domain.Error
// taxonomy (typed ErrorCode) is a behavior-visible change that belongs
// to the semantic-foundation release, not the facade removal.
package apperr

import (
	"errors"
	"fmt"
)

// Error codes for machine-readable error classification.
const (
	CodeNotFound       = "not_found"
	CodeConflict       = "conflict"
	CodeInvalidInput   = "invalid_input"
	CodeSchemaConflict = "schema_conflict"
	CodeUnauthorized   = "unauthorized"
	CodeInvalidSchema  = "invalid_schema"
	CodeInvalidGraph   = "invalid_graph"
	CodeCorruptedIndex = "corrupted_index"
	CodeInternalError  = "internal_error"
)

// Sentinel errors. Each is wrapped inside a DomainError by the corresponding
// New*Error constructor, so errors.Is(err, ErrNotFound) works through any
// number of fmt.Errorf("prefix: %w", err) wrappings.
var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrInvalidInput   = errors.New("invalid input")
	ErrSchemaConflict = errors.New("schema conflict")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrInvalidGraph   = errors.New("invalid graph")
	ErrCorruptedIndex = errors.New("corrupted index")
)

// DomainError is the canonical typed error for domain-level failures.
// It carries a machine-readable Code for HTTP/CLI status mapping,
// an optional Field for structured validation errors, a human-readable
// Message, and an optional wrapped underlying error.
type DomainError struct {
	Code    string
	Field   string
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Field)
	}
	return e.Message
}

func (e *DomainError) Unwrap() error { return e.Err }

// --- Constructor helpers ---

// NewNotFoundError returns a DomainError with CodeNotFound.
// The wrapped ErrNotFound sentinel lets callers use errors.Is.
func NewNotFoundError(msg string) *DomainError {
	return &DomainError{Code: CodeNotFound, Message: msg, Err: ErrNotFound}
}

// NewInvalidInputError returns a DomainError with CodeInvalidInput.
func NewInvalidInputError(msg string) *DomainError {
	return &DomainError{Code: CodeInvalidInput, Message: msg, Err: ErrInvalidInput}
}

// NewSchemaConflictError returns a DomainError with CodeSchemaConflict.
func NewSchemaConflictError(msg string) *DomainError {
	return &DomainError{Code: CodeSchemaConflict, Message: msg, Err: ErrSchemaConflict}
}

// NewInvalidSchemaError returns a DomainError with CodeInvalidSchema.
// Field and Value describe which schema element was violated.
func NewInvalidSchemaError(field, value string) *DomainError {
	return &DomainError{
		Code:    CodeInvalidSchema,
		Field:   field,
		Message: fmt.Sprintf("invalid %s: %s", field, value),
	}
}

// NewConflictError returns a DomainError with CodeConflict.
func NewConflictError(msg string) *DomainError {
	return &DomainError{Code: CodeConflict, Message: msg, Err: ErrConflict}
}

// NewInvalidGraphError returns a DomainError with CodeInvalidGraph.
func NewInvalidGraphError(msg string) *DomainError {
	return &DomainError{Code: CodeInvalidGraph, Message: msg, Err: ErrInvalidGraph}
}

// NewCorruptedIndexError returns a DomainError with CodeCorruptedIndex.
func NewCorruptedIndexError(msg string) *DomainError {
	return &DomainError{Code: CodeCorruptedIndex, Message: msg, Err: ErrCorruptedIndex}
}
