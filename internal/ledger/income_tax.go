package ledger

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// IAS 12 income-tax surface with fiscal-period guards. Both provisions post
// "now" (guarded against a closed current period).

// RunCurrentTax books the current tax provision for a period.
func (s *Service) RunCurrentTax(ctx context.Context, period string, taxableProfit, rate decimal.Decimal, actor string) (*repository.IncomeTaxRun, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.RunCurrentTax(ctx, period, taxableProfit, rate, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.tax.current",
		Message:   "current tax provision " + period,
	})
}

// RecognizeDeferredTax books deferred tax on a temporary difference.
func (s *Service) RecognizeDeferredTax(ctx context.Context, ref, description string, tempDiff, rate decimal.Decimal, dtype, actor string) (*repository.DeferredTaxItem, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.RecognizeDeferredTax(ctx, ref, description, tempDiff, rate, dtype, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.tax.deferred",
		Message:   "deferred tax " + ref,
	})
}

// ListIncomeTaxRuns returns recent current-tax runs.
func (s *Service) ListIncomeTaxRuns(ctx context.Context, limit int) ([]repository.IncomeTaxRun, error) {
	return s.repo.ListIncomeTaxRuns(ctx, limit)
}

// ListDeferredTaxItems returns recent deferred-tax items.
func (s *Service) ListDeferredTaxItems(ctx context.Context, limit int) ([]repository.DeferredTaxItem, error) {
	return s.repo.ListDeferredTaxItems(ctx, limit)
}
