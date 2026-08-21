package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

// PaymentsClearingAccount is the control account operational disbursements park
// in until finance matches them to the document they paid.
const PaymentsClearingAccount = "1050"

// SettlementInput is one settled disbursement from iag-payments.
type SettlementInput struct {
	InstructionID   string
	ReferenceNumber string
	OriginService   string
	Category        string
	PartyBusinessID string
	ProviderRef     string
	Amount          decimal.Decimal
	Currency        string
	// DebitAccount is where the money lands: the clearing account for anything
	// finance cannot classify from the settlement alone, or a real account for
	// the categories it can (coffee payouts capitalise straight to inventory).
	DebitAccount string
	DebitMemo    string
	Description  string
}

// BookPaymentSettlement books a settled disbursement — Dr the resolved account,
// Cr cash — and, when it lands in Payments Clearing, records the subledger row
// in the same transaction.
//
// The atomicity is the point. A GL debit to a control account with no subledger
// row behind it is exactly the failure the fleet-fuel path hit: the liability
// existed in the general ledger, was invisible to the subledger, and left a
// permanent unexplained difference on the control account. Writing both in one
// transaction means the reconciliation in ControlReconciliation is measuring
// real drift rather than a race.
func (s *Service) BookPaymentSettlement(ctx context.Context, ref EventRef, in SettlementInput) (*domain.JournalEntry, error) {
	if in.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}
	currency := in.Currency
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}

	resolved, err := s.resolveLines(ctx, []LineInput{
		{AccountCode: in.DebitAccount, Debit: in.Amount, Memo: in.DebitMemo},
		{AccountCode: cashAccount, Credit: in.Amount, Memo: "Cash disbursed"},
	})
	if err != nil {
		return nil, err
	}
	window, err := s.resolvePostingWindow(ctx, ref)
	if err != nil {
		return nil, err
	}

	// Only a clearing debit needs a subledger row; a settlement classified on
	// arrival has already reached its final account and nothing is pending.
	var side repository.BookSideEffect
	if in.DebitAccount == PaymentsClearingAccount {
		item := repository.PaymentsClearingInput{
			InstructionID:   in.InstructionID,
			ReferenceNumber: in.ReferenceNumber,
			OriginService:   in.OriginService,
			Category:        in.Category,
			PartyBusinessID: in.PartyBusinessID,
			ProviderRef:     in.ProviderRef,
			Amount:          in.Amount,
			Currency:        currency,
			SettledAt:       window.Transaction,
		}
		side = func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) error {
			return repository.AddPaymentsClearingItemTx(ctx, tx, item)
		}
	}

	return s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description:    describeDeferral(in.Description, window),
		SourceEventID:  &ref.ID,
		SourceService:  optionalString(ref.Source),
		CorrelationID:  optionalString(ref.CorrelationID),
		Currency:       currency,
		FXRate:         s.repo.RateOrOne(ctx, currency, window.Transaction),
		AccountingDate: window.Accounting,
		Lines:          resolved,
	}, ref.ID, ref.Type, time.Now().UTC(), side, &repository.AuditInfo{
		Actor:     "system:" + ref.Source,
		EventType: "ledger.payment.settled",
		Message:   in.Description,
	})
}

// ListPaymentsClearing returns the disbursements sitting in 1050, newest first.
func (s *Service) ListPaymentsClearing(ctx context.Context, entityIDs []uuid.UUID, status string, limit int) ([]repository.PaymentsClearingItem, error) {
	return s.repo.ListPaymentsClearing(ctx, entityIDs, status, limit)
}

// ClearPaymentsClearingItem records the document a settled disbursement paid.
func (s *Service) ClearPaymentsClearingItem(ctx context.Context, id uuid.UUID, documentRef string, by *uuid.UUID) (*repository.PaymentsClearingItem, error) {
	return s.repo.ClearPaymentsClearingItem(ctx, id, documentRef, by)
}
