package ledger

import (
	"context"

	"github.com/iag-finance/backend/internal/domain"
)

func (s *Service) ListBankAccounts(ctx context.Context, tenant string) ([]domain.BankAccount, error) {
	return s.repo.ListBankAccounts(ctx, tenant)
}

func (s *Service) ListCherryIntake(ctx context.Context, tenant string, limit int) ([]domain.CherryIntakeLine, error) {
	return s.repo.ListCherryIntake(ctx, tenant, limit)
}
