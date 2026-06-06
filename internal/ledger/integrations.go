package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
)

func (s *Service) EFRISCounts(ctx context.Context) (repository.EFRISStatus, error) {
	return s.repo.EFRISCounts(ctx)
}

func (s *Service) BankingCounts(ctx context.Context) (repository.BankingStatus, error) {
	return s.repo.BankingCounts(ctx)
}

func (s *Service) QueueEFRISSubmission(ctx context.Context, documentRef string) (uuid.UUID, error) {
	return s.repo.QueueEFRISSubmission(ctx, documentRef)
}

func (s *Service) ImportBankStatement(ctx context.Context, bankAccountCode string, statementDate time.Time, lineCount int) (uuid.UUID, error) {
	return s.repo.ImportBankStatement(ctx, bankAccountCode, statementDate, lineCount)
}
