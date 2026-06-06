package ledger

import (
	"context"

	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
)

func (s *Service) ListStatementLines(ctx context.Context, statementID uuid.UUID) ([]repository.StatementLine, error) {
	return s.repo.ListStatementLines(ctx, statementID)
}

func (s *Service) MatchStatementLine(ctx context.Context, lineID uuid.UUID, documentRef string) error {
	line, err := s.repo.GetStatementLine(ctx, lineID)
	if err != nil {
		return err
	}
	if line == nil {
		return repository.ErrStatementLineNotFound
	}
	if err := s.repo.MatchStatementLine(ctx, lineID, documentRef); err != nil {
		return err
	}
	code, err := s.repo.BankAccountCodeForStatement(ctx, line.StatementID)
	if err != nil {
		return err
	}
	return s.repo.MaterializeBankTransactions(ctx, code, line.StatementID)
}

func (s *Service) AutoMatchStatement(ctx context.Context, statementID uuid.UUID) (int, error) {
	return s.repo.AutoMatchStatementLines(ctx, statementID)
}
