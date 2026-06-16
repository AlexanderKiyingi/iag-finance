package ledger

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// ErrInvalidSettlement is returned when a bank line cannot be settled against a
// document — non-positive amount, or a bank direction that does not align with
// the document side (AR settles on a credit, AP on a debit).
var ErrInvalidSettlement = errors.New("statement line cannot settle the document")

func (s *Service) ListStatementLines(ctx context.Context, statementID uuid.UUID) ([]repository.StatementLine, error) {
	return s.repo.ListStatementLines(ctx, statementID)
}

// settleMatchedLine clears the AR/AP open item a matched bank line points to by
// booking the Cash↔AR/AP payment for the line amount. This is what makes
// reconciliation actually settle the receivable/payable instead of only
// recording a bank-ledger row. It is idempotent on the line id (paymentRef
// "recon:<lineID>"), so re-confirming or retrying never double-settles, and the
// payment path's over-application guard bounds duplicate matches to the same
// document. A line with no matched document (a plain 'add') settles nothing.
func (s *Service) settleMatchedLine(ctx context.Context, line *repository.StatementLine) error {
	docRef := strings.TrimSpace(line.MatchedDocumentRef)
	if docRef == "" {
		return nil
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(line.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidSettlement
	}
	paymentRef := "recon:" + line.ID.String()

	switch line.Direction {
	case "credit": // money into the bank → a customer paid us → settle AR
		ar, err := s.repo.GetARByDocumentRef(ctx, docRef)
		if err != nil {
			return err
		}
		if ar == nil {
			return repository.ErrOriginalNotFound
		}
		// nil outbox: reconciliation-driven settlement does not emit the
		// finance.payment.made signal (the interactive payment endpoints do).
		_, _, err = s.ApplyARPayment(ctx, ar.ID, amount, ar.Currency, paymentRef, "reconciliation", nil)
		return err
	case "debit": // money out of the bank → we paid a vendor → settle AP
		ap, err := s.repo.GetAPByDocumentRef(ctx, docRef)
		if err != nil {
			return err
		}
		if ap == nil {
			return repository.ErrOriginalNotFound
		}
		_, _, err = s.ApplyAPPayment(ctx, ap.ID, amount, ap.Currency, paymentRef, "reconciliation", nil)
		return err
	default:
		return ErrInvalidSettlement
	}
}

// MatchStatementLine matches an unmatched line to a document, settles the open
// item it points to, then records the bank-ledger row. Settlement runs first so
// a failed/invalid match never marks the line matched.
func (s *Service) MatchStatementLine(ctx context.Context, lineID uuid.UUID, documentRef string) error {
	line, err := s.repo.GetStatementLine(ctx, lineID)
	if err != nil {
		return err
	}
	if line == nil {
		return repository.ErrStatementLineNotFound
	}
	// A manual match is authoritative for the document reference.
	line.MatchedDocumentRef = strings.TrimSpace(documentRef)
	if err := s.settleMatchedLine(ctx, line); err != nil {
		return err
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

// ConfirmStatementLine settles a reviewer-approved draft match and promotes it
// to confirmed. Settlement runs before the status flip so a retry after a
// transient failure still finds the line in 'proposed' and can complete it.
func (s *Service) ConfirmStatementLine(ctx context.Context, lineID uuid.UUID) error {
	line, err := s.repo.GetStatementLine(ctx, lineID)
	if err != nil {
		return err
	}
	if line == nil || line.MatchStatus != "proposed" {
		return repository.ErrStatementLineNotFound
	}
	if err := s.settleMatchedLine(ctx, line); err != nil {
		return err
	}
	confirmed, err := s.repo.ConfirmStatementLineMatch(ctx, lineID)
	if err != nil {
		return err
	}
	code, err := s.repo.BankAccountCodeForStatement(ctx, confirmed.StatementID)
	if err != nil {
		return err
	}
	return s.repo.MaterializeBankTransactions(ctx, code, confirmed.StatementID)
}

// RejectStatementLine discards a draft match, returning the line to unmatched.
func (s *Service) RejectStatementLine(ctx context.Context, lineID uuid.UUID) error {
	return s.repo.RejectStatementLineMatch(ctx, lineID)
}
