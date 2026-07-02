package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// IAS 37 provisions surface. All movements post "now" and honour the fiscal
// period close.

func (s *Service) RecognizeProvision(ctx context.Context, in repository.RecognizeProvisionInput, actor string) (*repository.LiabProvision, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.RecognizeProvision(ctx, in, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.provision.recognize", Message: "recognise provision " + in.Description,
	})
}

func (s *Service) UnwindProvision(ctx context.Context, id uuid.UUID, reference, actor string) (*repository.LiabProvision, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.UnwindProvisionDiscount(ctx, id, reference, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.provision.unwind", Message: "unwind provision",
	})
}

func (s *Service) UtilizeProvision(ctx context.Context, id uuid.UUID, amount decimal.Decimal, reference, actor string) (*repository.LiabProvision, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.UtilizeProvision(ctx, id, amount, reference, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.provision.utilize", Message: "utilise provision",
	})
}

func (s *Service) RemeasureProvision(ctx context.Context, id uuid.UUID, newEstimate decimal.Decimal, reference, actor string) (*repository.LiabProvision, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.RemeasureProvision(ctx, id, newEstimate, reference, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.provision.remeasure", Message: "remeasure provision",
	})
}

func (s *Service) ReverseProvision(ctx context.Context, id uuid.UUID, reference, actor string) (*repository.LiabProvision, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.ReverseProvision(ctx, id, reference, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.provision.reverse", Message: "reverse provision",
	})
}

func (s *Service) ListProvisions(ctx context.Context, limit int) ([]repository.LiabProvision, error) {
	return s.repo.ListProvisions(ctx, limit)
}
