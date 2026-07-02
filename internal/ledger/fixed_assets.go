package ledger

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// RegisterFixedAsset capitalises a warehouse asset into the finance subledger so
// it can be depreciated. When in.CapitalizeFromAccount is set it also posts the
// reclass Dr Fixed Assets / Cr <expense> for the cost (as of the in-service
// date), refusing if that period is closed; empty leaves it record-only.
func (s *Service) RegisterFixedAsset(ctx context.Context, in repository.CreateFixedAssetInput) (*repository.FixedAsset, error) {
	if in.CapitalizeFromAccount != "" {
		closed, err := s.repo.IsPeriodClosed(ctx, in.InServiceDate.Format("2006-01"))
		if err != nil {
			return nil, err
		}
		if closed {
			return nil, ErrPeriodClosed
		}
	}
	return s.repo.CreateFixedAsset(ctx, in)
}

func (s *Service) ListFixedAssets(ctx context.Context, limit, offset int) ([]repository.FixedAsset, error) {
	return s.repo.ListFixedAssets(ctx, limit, offset)
}

// RunDepreciation posts depreciation (straight-line or reducing-balance per each
// asset's method) for the given 'YYYY-MM' period, refusing a closed period.
func (s *Service) RunDepreciation(ctx context.Context, period string) (*repository.DepreciationRun, error) {
	closed, err := s.repo.IsPeriodClosed(ctx, period)
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.RunDepreciation(ctx, period, time.Now().UTC())
}

// ImpairAsset writes an asset down to its recoverable amount (IAS 36), dated
// effective, refusing a closed period.
func (s *Service) ImpairAsset(ctx context.Context, assetRef string, recoverable decimal.Decimal, effective time.Time, actor string) (*repository.FixedAsset, error) {
	if err := s.guardOpen(ctx, effective); err != nil {
		return nil, err
	}
	return s.repo.BookImpairment(ctx, assetRef, recoverable, effective, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.asset.impair", Message: "impair " + assetRef,
	})
}

// ReverseImpairment reverses a prior impairment (IAS 36), refusing a closed period.
func (s *Service) ReverseImpairment(ctx context.Context, assetRef string, amount decimal.Decimal, effective time.Time, actor string) (*repository.FixedAsset, error) {
	if err := s.guardOpen(ctx, effective); err != nil {
		return nil, err
	}
	return s.repo.ReverseImpairment(ctx, assetRef, amount, effective, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.asset.impair.reverse", Message: "reverse impairment " + assetRef,
	})
}

// RevalueAsset restates an asset to a new carrying amount (IAS 16 revaluation
// model), refusing a closed period.
func (s *Service) RevalueAsset(ctx context.Context, assetRef string, newCarrying decimal.Decimal, effective time.Time, actor string) (*repository.FixedAsset, error) {
	if err := s.guardOpen(ctx, effective); err != nil {
		return nil, err
	}
	return s.repo.BookRevaluation(ctx, assetRef, newCarrying, effective, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.asset.revalue", Message: "revalue " + assetRef,
	})
}
