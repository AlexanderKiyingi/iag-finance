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

func (s *Service) GetEFRISSubmission(ctx context.Context, documentRef string) (repository.EFRISSubmissionState, error) {
	return s.repo.GetEFRISSubmission(ctx, documentRef)
}

// EnqueueOutbox durably records a standalone domain event for the relay worker.
func (s *Service) EnqueueOutbox(ctx context.Context, ev repository.OutboxEvent) error {
	return s.repo.EnqueueOutbox(ctx, ev)
}

// BaseCurrency is the configured reporting currency.
func (s *Service) BaseCurrency() string { return s.repo.BaseCurrency() }

// ListExchangeRates returns recent FX rates.
func (s *Service) ListExchangeRates(ctx context.Context, limit int) ([]repository.ExchangeRate, error) {
	return s.repo.ListRates(ctx, limit)
}

// UpsertExchangeRate records a currency→base rate effective on a date.
func (s *Service) UpsertExchangeRate(ctx context.Context, currency, rate string, asOf time.Time) error {
	return s.repo.UpsertRate(ctx, currency, rate, asOf)
}

func (s *Service) ImportBankStatement(ctx context.Context, bankAccountCode string, statementDate time.Time, lineCount int) (uuid.UUID, error) {
	return s.repo.ImportBankStatement(ctx, bankAccountCode, statementDate, lineCount)
}
