package domain

import (
	"errors"
	"fmt"
)

// ErrorCode identifies a stable domain failure category.
type ErrorCode string

const (
	ErrorNotFound       ErrorCode = "not_found"
	ErrorInvalidInput   ErrorCode = "invalid_input"
	ErrorConflict       ErrorCode = "conflict"
	ErrorSchemaConflict ErrorCode = "schema_conflict"
	ErrorUnauthorized   ErrorCode = "unauthorized"
	ErrorInvalidSchema  ErrorCode = "invalid_schema"
	ErrorInvalidGraph   ErrorCode = "invalid_graph"
	ErrorCorruptedIndex ErrorCode = "corrupted_index"
	ErrorInternal       ErrorCode = "internal_error"
)

// Sentinel errors support errors.Is checks without coupling callers to
// transport or storage implementations.
var (
	ErrNotFound       = errors.New("domain: not found")
	ErrInvalidInput   = errors.New("domain: invalid input")
	ErrConflict       = errors.New("domain: conflict")
	ErrSchemaConflict = errors.New("domain: schema conflict")
	ErrUnauthorized   = errors.New("domain: unauthorized")
	ErrInvalidSchema  = errors.New("domain: invalid schema")
	ErrInvalidGraph   = errors.New("domain: invalid graph")
	ErrCorruptedIndex = errors.New("domain: corrupted index")
	ErrInternal       = errors.New("domain: internal error")
)

// Error is a structured domain error with a stable code and optional field.
type Error struct {
	Code    ErrorCode
	Message string
	Field   string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// NewError creates a domain error with an optional underlying cause.
func NewError(code ErrorCode, message, field string, cause error) *Error {
	return &Error{Code: code, Message: message, Field: field, Cause: cause}
}
