package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

var (
	ErrOpenItemNotFound = repository.ErrOpenItemNotFound
	ErrPaymentExceeds   = repository.ErrPaymentExceeds
	// ErrCurrencyMismatch rejects a payment whose currency differs from the open
	// item's. Cross-currency settlement is not auto-converted (it would need an
	// FX gain/loss line); pay in the item's currency.
	ErrCurrencyMismatch = errors.New("payment currency must match the open item currency")
)

// ApplyARPayment records a customer receipt and books Cash / AR atomically. When
// outbox is non-nil it is enqueued in the same transaction as a settlement
// signal (finance.payment.made).
func (s *Service) ApplyARPayment(ctx context.Context, itemID uuid.UUID, amount decimal.Decimal, currency, paymentRef, actor string, outbox *repository.OutboxEvent) (*domain.Payment, *domain.AROpenItem, error) {
	item, err := s.repo.GetAROpenItem(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, ErrOpenItemNotFound
	}
	if currency == "" {
		currency = item.Currency
	} else if currency != item.Currency {
		return nil, nil, ErrCurrencyMismatch
	}
	// Settle at the document's booking rate (historical method) so the AR base
	// balance clears exactly and the books stay balanced in base currency.
	fxRate, err := s.repo.OpenItemFXRate(ctx, "ar", itemID)
	if err != nil {
		return nil, nil, err
	}
	eventID := fmt.Sprintf("payment.ar:%s:%s", itemID.String(), paymentRef)
	lines, err := s.resolveLines(ctx, []LineInput{
		{AccountCode: "1000", Debit: amount, Memo: "Cash receipt"},
		{AccountCode: "1100", Credit: amount, Memo: "AR clearance"},
	})
	if err != nil {
		return nil, nil, err
	}
	payment, err := s.repo.ApplyPaymentWithJournal(ctx, repository.PaymentWithJournalParams{
		EventID: eventID, EventType: "payment.received", Source: "iag.finance", CorrelationID: paymentRef,
		Description: fmt.Sprintf("AR payment — %s", item.DocumentRef),
		Direction: "ar", OpenItemID: itemID, Amount: amount, Currency: currency, PaymentRef: paymentRef,
		FXRate: fxRate, Lines: lines, Outbox: outbox,
	}, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "ar.payment",
		Message:   fmt.Sprintf("AR payment %s on %s for %s %s", paymentRef, item.DocumentRef, currency, amount.String()),
	})
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.repo.GetAROpenItem(ctx, itemID)
	return payment, updated, err
}

// ApplyAPPayment records a vendor disbursement and books AP / Cash atomically.
// When outbox is non-nil it is enqueued in the same transaction as a settlement
// signal (finance.payment.made).
func (s *Service) ApplyAPPayment(ctx context.Context, itemID uuid.UUID, amount decimal.Decimal, currency, paymentRef, actor string, outbox *repository.OutboxEvent) (*domain.Payment, *domain.APOpenItem, error) {
	item, err := s.repo.GetAPOpenItem(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, ErrOpenItemNotFound
	}
	if currency == "" {
		currency = item.Currency
	} else if currency != item.Currency {
		return nil, nil, ErrCurrencyMismatch
	}
	fxRate, err := s.repo.OpenItemFXRate(ctx, "ap", itemID)
	if err != nil {
		return nil, nil, err
	}
	eventID := fmt.Sprintf("payment.ap:%s:%s", itemID.String(), paymentRef)
	lines, err := s.resolveLines(ctx, []LineInput{
		{AccountCode: "2000", Debit: amount, Memo: "AP clearance"},
		{AccountCode: "1000", Credit: amount, Memo: "Cash disbursement"},
	})
	if err != nil {
		return nil, nil, err
	}
	payment, err := s.repo.ApplyPaymentWithJournal(ctx, repository.PaymentWithJournalParams{
		EventID: eventID, EventType: "payment.disbursed", Source: "iag.finance", CorrelationID: paymentRef,
		Description: fmt.Sprintf("AP payment — %s", item.DocumentRef),
		Direction: "ap", OpenItemID: itemID, Amount: amount, Currency: currency, PaymentRef: paymentRef,
		FXRate: fxRate, Lines: lines, Outbox: outbox,
	}, &repository.AuditInfo{
		Actor:     actorOrSystem(actor),
		EventType: "ap.payment",
		Message:   fmt.Sprintf("AP payment %s on %s for %s %s", paymentRef, item.DocumentRef, currency, amount.String()),
	})
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.repo.GetAPOpenItem(ctx, itemID)
	return payment, updated, err
}

func (s *Service) ListPaymentsForItem(ctx context.Context, openItemID uuid.UUID) ([]domain.Payment, error) {
	return s.repo.ListPaymentsForItem(ctx, openItemID)
}

// ParsePaymentAmount validates a payment amount string and quantizes it to 2dp.
// Rounding to the stored scale (NUMERIC(18,2)) before any balance comparison
// keeps the in-memory running balance and the persisted amount from diverging
// on sub-cent inputs.
func ParsePaymentAmount(raw string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, err
	}
	d = d.Round(2)
	if d.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, errors.New("amount must be positive")
	}
	return d, nil
}
