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
)

// ApplyARPayment records a customer receipt and books Cash / AR atomically.
func (s *Service) ApplyARPayment(ctx context.Context, itemID uuid.UUID, amount decimal.Decimal, currency, paymentRef string) (*domain.Payment, *domain.AROpenItem, error) {
	item, err := s.repo.GetAROpenItem(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, ErrOpenItemNotFound
	}
	if currency == "" {
		currency = item.Currency
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
		Lines: lines,
	})
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.repo.GetAROpenItem(ctx, itemID)
	return payment, updated, err
}

// ApplyAPPayment records a vendor disbursement and books AP / Cash atomically.
func (s *Service) ApplyAPPayment(ctx context.Context, itemID uuid.UUID, amount decimal.Decimal, currency, paymentRef string) (*domain.Payment, *domain.APOpenItem, error) {
	item, err := s.repo.GetAPOpenItem(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if item == nil {
		return nil, nil, ErrOpenItemNotFound
	}
	if currency == "" {
		currency = item.Currency
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
		Lines: lines,
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

// ParsePaymentAmount validates a payment amount string.
func ParsePaymentAmount(raw string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, err
	}
	if d.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, errors.New("amount must be positive")
	}
	return d, nil
}
