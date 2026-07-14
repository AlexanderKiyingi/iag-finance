package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/repository"
)

// RecordLateFee charges a late fee and posts Dr 1100 AR / Cr 4300 Late Fee Income
// as of the fee date, refusing a closed period.
func (s *Service) RecordLateFee(ctx context.Context, in repository.CreateLateFeeInput) (*repository.LateFee, error) {
	closed, err := s.repo.IsPeriodClosed(ctx, in.FeeDate.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.CreateLateFee(ctx, in)
}

func (s *Service) ListLateFees(ctx context.Context, limit, offset int) ([]repository.LateFee, error) {
	return s.repo.ListLateFees(ctx, limit, offset)
}
