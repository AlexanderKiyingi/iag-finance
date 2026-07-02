package ledger

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// IAS 1 matching — prepaid-expense amortization surface with fiscal-period
// guards. Capitalization posts "now"; an amortization run is dated to its period
// end so the expense lands in the period it covers.

// AmortizationResult summarises an amortization run.
type AmortizationResult struct {
	Period string               `json:"period"`
	Amount decimal.Decimal      `json:"amount"`
	Lines  int                  `json:"lines"`
	Entry  *domain.JournalEntry `json:"entry,omitempty"`
}

// CreatePrepayment capitalises a prepayment and lays out its amortization plan,
// refusing if the current period is closed.
func (s *Service) CreatePrepayment(ctx context.Context, in repository.CreatePrepaymentInput, actor string) (*repository.PrepaidSchedule, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.CreatePrepayment(ctx, in, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.prepayment.capitalize",
		Message:   "capitalize prepayment " + in.SourceRef,
	})
}

// RunAmortization expenses all straight-line slices due on/before the period.
func (s *Service) RunAmortization(ctx context.Context, period, actor string) (*AmortizationResult, error) {
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
	entry, amount, n, err := s.repo.RunAmortization(ctx, period, end, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.prepayment.amortize",
		Message:   "amortize prepayments " + period,
	})
	if err != nil {
		return nil, err
	}
	return &AmortizationResult{Period: period, Amount: amount, Lines: n, Entry: entry}, nil
}

// ListPrepayments returns recent schedules.
func (s *Service) ListPrepayments(ctx context.Context, limit int) ([]repository.PrepaidSchedule, error) {
	return s.repo.ListPrepayments(ctx, limit)
}
