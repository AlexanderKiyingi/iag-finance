package ledger

import (
	"context"
	"time"

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

// RunDepreciation posts straight-line depreciation for the given 'YYYY-MM'
// period, refusing a period that has been closed.
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
