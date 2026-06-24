// Package server: request and response type aliases.
//
// The canonical request/response structs (StoreRequest, SearchRequest, etc.) are defined
// in the core package because they are also used by the CLI handlers in main.go.
// This file documents the contract and provides compile-time assertions that the
// server surfaces all expected types.
package server

import "github.com/pavelveter/hermem/src/internal/core"

// Compile-time assertion: every handler accommodates a core.* request type.
var (
	_ = core.StoreRequest{}
	_ = core.SearchRequest{}
	_ = core.RetrieveRequest{}
	_ = core.IngestRequest{}
	_ = core.EdgeRequest{}
	_ = core.TaskStatusRequest{}
	_ = core.TaskListRequest{}
	_ = core.TaskShowRequest{}
	_ = core.TaskDepRequest{}
	_ = core.TaskRollbackRequest{}
	_ = core.TaskTreeRequest{}
	_ = core.TaskCreateRequest{}
	_ = core.TaskExecutableResponse{}
	_ = core.TaskShowResponse{}
	_ = core.TaskRollbackResponse{}
	_ = core.TaskTreeResponse{}
	_ = core.TaskCreateResponse{}
	_ = core.ErrorResponse{}
)
