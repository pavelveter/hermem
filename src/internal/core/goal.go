package core

import "github.com/pavelveter/hermem/pkg/domain"

// Goal captures the lifecycle state of a goal — a goal IS A Task in
// this schema, distinguished only by Entity.Category == "goal" at
// the parent row.
//
// Deprecated: alias to the canonical pkg/domain.Goal.
type Goal = domain.Goal
