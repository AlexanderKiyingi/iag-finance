package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// Three-way match surface: run detection, review/resolve exceptions, and write a
// confirmed GR/IR residual to Purchase Price Variance.

// RunMatchCheck classifies GR/IR accruals and raises variance/orphan exceptions.
func (s *Service) RunMatchCheck(ctx context.Context) (int, error) {
	return s.repo.DetectMatchExceptions(ctx)
}

// ListMatchExceptions returns the review queue.
func (s *Service) ListMatchExceptions(ctx context.Context, status string, limit int) ([]repository.MatchException, error) {
	return s.repo.ListMatchExceptions(ctx, status, limit)
}

// ResolveMatchException marks an exception resolved.
func (s *Service) ResolveMatchException(ctx context.Context, id uuid.UUID, actor string) error {
	return s.repo.ResolveMatchException(ctx, id, actorOrSystem(actor))
}

// WriteOffMatchVariance writes a PO's residual GR/IR balance to PPV, refusing a
// closed period.
func (s *Service) WriteOffMatchVariance(ctx context.Context, poRef, actor string) (*domain.JournalEntry, error) {
	now := time.Now().UTC()
	if err := s.guardOpen(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.WriteOffGRNIVariance(ctx, poRef, now, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "finance.grir.variance", Message: "write off GR/IR variance PO " + poRef,
	})
}
