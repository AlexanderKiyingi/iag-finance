package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// IFRS 9 provisioning surface. All postings honour the fiscal-period close: an
// ECL run is dated to the period end, write-offs/recoveries to "now".

// RunECLProvision computes the expected-credit-loss allowance target and books
// the movement to it for the given 'YYYY-MM' period, refusing a closed period.
func (s *Service) RunECLProvision(ctx context.Context, period, actor string) (*repository.ECLProvision, error) {
	end, err := periodEnd(period)
	if err != nil {
		return nil, err
	}
	closed, err := s.repo.IsPeriodClosed(ctx, period)
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.BookECLProvision(ctx, period, end, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.ecl.provision",
		Message:   "ECL provision " + period,
	})
}

// WriteOffAR de-recognises an open receivable against the allowance (remainder
// to bad-debt expense), refusing if the current period is closed.
func (s *Service) WriteOffAR(ctx context.Context, documentRef, reason, actor string) (*repository.ARWriteOff, error) {
	now := time.Now().UTC()
	closed, err := s.repo.IsPeriodClosed(ctx, now.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.WriteOffReceivable(ctx, documentRef, reason, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.ar.writeoff",
		Message:   fmt.Sprintf("write-off %s: %s", documentRef, reason),
	})
}

// RecoverAR books cash recovered on a written-off debt as income. reference is
// the caller's idempotency key for the recovery.
func (s *Service) RecoverAR(ctx context.Context, documentRef, reference string, amount decimal.Decimal, actor string) (*domain.JournalEntry, error) {
	now := time.Now().UTC()
	closed, err := s.repo.IsPeriodClosed(ctx, now.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodClosed
	}
	return s.repo.RecoverWrittenOff(ctx, documentRef, reference, amount, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.ar.recovery",
		Message:   fmt.Sprintf("recovery %s ref %s", documentRef, reference),
	})
}

// ListECLProvisions returns recent provisioning runs.
func (s *Service) ListECLProvisions(ctx context.Context, limit int) ([]repository.ECLProvision, error) {
	return s.repo.ListECLProvisions(ctx, limit)
}

// periodEnd returns the last calendar day of a 'YYYY-MM' period.
func periodEnd(period string) (time.Time, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, fmt.Errorf("period must be YYYY-MM: %w", err)
	}
	return t.AddDate(0, 1, -1), nil
}
