package memory

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/tracing"
)

func StoreWithTracing(ctx context.Context, s *Service, req core.StoreRequest, schema core.SchemaConfig) error {
	t := tracing.TracerFrom(ctx)
	ctx, span := t.StartSpan(ctx, "memory.Store")
	defer span.End()
	span.SetAttribute("entity_id", req.ID)
	span.SetAttribute("category", req.Category)
	err := s.Store(ctx, req, schema)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func StoreAndLinkWithTracing(ctx context.Context, s *Service, req core.StoreRequest, schema core.SchemaConfig) error {
	t := tracing.TracerFrom(ctx)
	ctx, span := t.StartSpan(ctx, "memory.StoreAndLink")
	defer span.End()
	span.SetAttribute("entity_id", req.ID)
	span.SetAttribute("category", req.Category)
	err := s.StoreAndLink(ctx, req, schema)
	if err != nil {
		span.RecordError(err)
	}
	return err
}
