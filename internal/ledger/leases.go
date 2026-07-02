package ledger

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// IFRS 16 lease surface with fiscal-period guards. Recognition posts "now"; a
// lease run is dated to its period end so interest/depreciation land in-period.

// LeaseRunResult summarises a lease run.
type LeaseRunResult struct {
	Period string               `json:"period"`
	Amount decimal.Decimal      `json:"amount"`
	Lines  int                  `json:"lines"`
	Entry  *domain.JournalEntry `json:"entry,omitempty"`
}

// CreateLease recognises a lease (ROU asset + lease liability), refusing if the
// current period is closed.
func (s *Service) CreateLease(ctx context.Context, in repository.CreateLeaseInput, actor string) (*repository.Lease, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.CreateLease(ctx, in, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.lease.recognize",
		Message:   "recognise lease " + in.LeaseRef,
	})
}

// RunLeasePeriod books interest, payment and depreciation for all due lease lines.
func (s *Service) RunLeasePeriod(ctx context.Context, period, actor string) (*LeaseRunResult, error) {
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
	entry, amount, n, err := s.repo.RunLeasePeriod(ctx, period, end, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.lease.run",
		Message:   "lease period " + period,
	})
	if err != nil {
		return nil, err
	}
	return &LeaseRunResult{Period: period, Amount: amount, Lines: n, Entry: entry}, nil
}

// ListLeases returns recent leases.
func (s *Service) ListLeases(ctx context.Context, limit int) ([]repository.Lease, error) {
	return s.repo.ListLeases(ctx, limit)
}
