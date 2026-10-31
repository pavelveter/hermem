package components

import (
	"context"

	consolidationdomain "github.com/pavelveter/hermem/src/internal/consolidation"
	"github.com/pavelveter/hermem/src/internal/util/safego"
)

type ConsolidationComponent struct {
	svc    *consolidationdomain.Service
	policy consolidationdomain.Policy
}

func NewConsolidationComponent(svc *consolidationdomain.Service, policy consolidationdomain.Policy) *ConsolidationComponent {
	return &ConsolidationComponent{svc: svc, policy: policy}
}

func (c *ConsolidationComponent) Start(ctx context.Context) error {
	safego.Go(ctx, "consolidation", func(ctx context.Context) {
		c.svc.Run(ctx, c.policy)
	})
	return nil
}

func (c *ConsolidationComponent) Stop(_ context.Context) error {
	return nil
}
