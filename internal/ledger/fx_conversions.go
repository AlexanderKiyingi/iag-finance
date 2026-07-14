package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/repository"
)

// RecordFXConversion persists a treasury currency conversion (record-only; the
// cash/GL impact flows through bank reconciliation — see migration 061).
func (s *Service) RecordFXConversion(ctx context.Context, in repository.CreateFXConversionInput) (*repository.FXConversion, error) {
	return s.repo.CreateFXConversion(ctx, in)
}

func (s *Service) ListFXConversions(ctx context.Context, limit, offset int) ([]repository.FXConversion, error) {
	return s.repo.ListFXConversions(ctx, limit, offset)
}
