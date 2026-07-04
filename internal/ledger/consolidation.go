package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// IFRS 10 consolidation elimination pass-throughs (report time).

// ConsolidationEliminations returns the full elimination summary for a scope as
// of a date.
func (s *Service) ConsolidationEliminations(ctx context.Context, asOf *time.Time, scope []uuid.UUID) (repository.ConsolidationSummary, error) {
	return s.repo.ConsolidationEliminations(ctx, asOf, scope)
}

// TransactionalEliminations returns the intra-group per-account reversals over a
// window for a scope.
func (s *Service) TransactionalEliminations(ctx context.Context, from, to *time.Time, scope []uuid.UUID) ([]repository.EliminationRow, error) {
	return s.repo.TransactionalEliminations(ctx, from, to, scope)
}

// StructuralEliminations returns the investment/equity/NCI/goodwill adjustments
// for a scope as of a date.
func (s *Service) StructuralEliminations(ctx context.Context, asOf *time.Time, scope []uuid.UUID) ([]repository.EliminationRow, decimal.Decimal, decimal.Decimal, error) {
	return s.repo.StructuralEliminations(ctx, asOf, scope)
}
