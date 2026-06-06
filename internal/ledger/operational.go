package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/domain"
)

func (s *Service) ListBankAccounts(ctx context.Context) ([]domain.BankAccount, error) {
	return s.repo.ListBankAccounts(ctx)
}

func (s *Service) ListCherryIntake(ctx context.Context, limit int) ([]domain.CherryIntakeLine, error) {
	return s.repo.ListCherryIntake(ctx, limit)
}
