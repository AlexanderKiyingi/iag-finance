package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// IFRS 15 recognition surface with fiscal-period guards. Deferral, accrual and
// milestone recognition post "now"; a ratable recognition run is dated to its
// period end.

// RecognitionResult summarises a recognition run.
type RecognitionResult struct {
	Period string               `json:"period"`
	Amount decimal.Decimal      `json:"amount"`
	Lines  int                  `json:"lines"`
	Entry  *domain.JournalEntry `json:"entry,omitempty"`
}

// CreateRevenueSchedule defers recognised revenue and lays out its recognition
// plan, refusing if the current period is closed.
func (s *Service) CreateRevenueSchedule(ctx context.Context, in repository.CreateScheduleInput, actor string) (*repository.RevenueSchedule, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.CreateSchedule(ctx, in, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.revenue.defer",
		Message:   "defer revenue " + in.SourceRef,
	})
}

// RunRevenueRecognition releases all ratable slices due on/before the period.
func (s *Service) RunRevenueRecognition(ctx context.Context, period, actor string) (*RecognitionResult, error) {
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
	entry, amount, n, err := s.repo.RunRecognition(ctx, period, end, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.revenue.recognize",
		Message:   "recognise revenue " + period,
	})
	if err != nil {
		return nil, err
	}
	return &RecognitionResult{Period: period, Amount: amount, Lines: n, Entry: entry}, nil
}

// SatisfyObligation recognises a milestone performance obligation.
func (s *Service) SatisfyObligation(ctx context.Context, id uuid.UUID, actor string) (*domain.JournalEntry, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.SatisfyObligation(ctx, id, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.revenue.milestone",
		Message:   fmt.Sprintf("satisfy obligation %s", id),
	})
}

// AccrueRevenue recognises revenue earned before billing (contract asset).
func (s *Service) AccrueRevenue(ctx context.Context, ref string, amount decimal.Decimal, actor string) (*domain.JournalEntry, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.AccrueRevenue(ctx, ref, amount, now, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "finance.revenue.accrue",
		Message:   "accrue revenue " + ref,
	})
}

// ListRevenueSchedules returns recent schedules.
func (s *Service) ListRevenueSchedules(ctx context.Context, limit int) ([]repository.RevenueSchedule, error) {
	return s.repo.ListSchedules(ctx, limit)
}

// guardOpen refuses when the period covering t is closed.
func (s *Service) guardOpen(ctx context.Context, t time.Time) error {
	closed, err := s.repo.IsPeriodClosed(ctx, t.Format("2006-01"))
	if err != nil {
		return err
	}
	if closed {
		return ErrPeriodClosed
	}
	return nil
}
