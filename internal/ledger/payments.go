package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

func decPtr(d decimal.Decimal) *decimal.Decimal { return &d }

// resolveAccount resolves a chart-of-accounts code or returns ErrAccountNotFound.
func (s *Service) resolveAccount(ctx context.Context, code string) (*domain.ChartAccount, error) {
	acct, err := s.repo.GetAccountByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, code)
	}
	return acct, nil
}

// settlementLines builds the GL lines for settling a foreign open item with
// realized FX gain/loss: the cash leg is valued at payRate (payment-date) and
// the AR/AP clearing leg at docRate (the document's booked rate); the base
// residual posts to Realized FX Gain (7200) or Loss (7210). For base currency or
// an unchanged rate the residual is zero and no FX line is added (behaves exactly
// as the historical method did before). All amounts balance in base currency,
// which is what the balanced-entry trigger now asserts (migration 028).
func (s *Service) settlementLines(ctx context.Context, direction string, amount decimal.Decimal, currency string, docRate, payRate decimal.Decimal) ([]repository.ResolvedLine, error) {
	cashBase := amount.Mul(payRate).Round(2)
	clearBase := amount.Mul(docRate).Round(2)

	cash, err := s.resolveAccount(ctx, "1000")
	if err != nil {
		return nil, err
	}
	var lines []repository.ResolvedLine
	var residual decimal.Decimal
	if direction == "ar" {
		ar, err := s.resolveAccount(ctx, "1100")
		if err != nil {
			return nil, err
		}
		lines = []repository.ResolvedLine{
			{AccountID: cash.ID, Debit: amount, Currency: currency, DebitBase: decPtr(cashBase), Memo: "Cash receipt", LineOrder: 0},
			{AccountID: ar.ID, Credit: amount, Currency: currency, CreditBase: decPtr(clearBase), Memo: "AR clearance", LineOrder: 1},
		}
		residual = cashBase.Sub(clearBase) // cash worth more than AR cleared → gain
	} else {
		ap, err := s.resolveAccount(ctx, "2000")
		if err != nil {
			return nil, err
		}
		lines = []repository.ResolvedLine{
			{AccountID: ap.ID, Debit: amount, Currency: currency, DebitBase: decPtr(clearBase), Memo: "AP clearance", LineOrder: 0},
			{AccountID: cash.ID, Credit: amount, Currency: currency, CreditBase: decPtr(cashBase), Memo: "Cash disbursement", LineOrder: 1},
		}
		residual = clearBase.Sub(cashBase) // liability cleared > cash paid → gain
	}

	if !residual.IsZero() {
		base := s.repo.BaseCurrency()
		if residual.IsPositive() {
			gain, err := s.resolveAccount(ctx, "7200")
			if err != nil {
				return nil, err
			}
			lines = append(lines, repository.ResolvedLine{
				AccountID: gain.ID, Credit: residual, Currency: base, CreditBase: decPtr(residual),
				Memo: "Realized FX gain", LineOrder: 2,
			})
		} else {
			loss, err := s.resolveAccount(ctx, "7210")
			if err != nil {
				return nil, err
			}
			amt := residual.Abs()
			lines = append(lines, repository.ResolvedLine{
				AccountID: loss.ID, Debit: amt, Currency: base, DebitBase: decPtr(amt),
				Memo: "Realized FX loss", LineOrder: 2,
			})
		}
	}
	return lines, nil
}

// settlementRates returns (docRate, payRate) for a settlement: the document's
// booked rate and the current (payment-date) rate. A missing current rate falls
// back to the document rate (→ no FX gain/loss), never to 1.
func (s *Service) settlementRates(ctx context.Context, direction string, itemID uuid.UUID, currency string) (decimal.Decimal, decimal.Decimal, error) {
	docRate, err := s.repo.OpenItemFXRate(ctx, direction, itemID)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	payRate, err := s.repo.GetRate(ctx, currency, time.Now().UTC())
	if err != nil {
		payRate = docRate
	}
	return docRate, payRate, nil
}

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
	// Settle the AR-clearing leg at the document rate and the cash leg at the
	// current rate; the base residual is realized FX gain/loss.
	docRate, payRate, err := s.settlementRates(ctx, "ar", itemID, currency)
	if err != nil {
		return nil, nil, err
	}
	eventID := fmt.Sprintf("payment.ar:%s:%s", itemID.String(), paymentRef)
	lines, err := s.settlementLines(ctx, "ar", amount, currency, docRate, payRate)
	if err != nil {
		return nil, nil, err
	}
	payment, err := s.repo.ApplyPaymentWithJournal(ctx, repository.PaymentWithJournalParams{
		EventID: eventID, EventType: "payment.received", Source: "iag.finance", CorrelationID: paymentRef,
		Description: fmt.Sprintf("AR payment — %s", item.DocumentRef),
		Direction: "ar", OpenItemID: itemID, Amount: amount, Currency: currency, PaymentRef: paymentRef,
		FXRate: docRate, Lines: lines, Outbox: outbox,
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
	docRate, payRate, err := s.settlementRates(ctx, "ap", itemID, currency)
	if err != nil {
		return nil, nil, err
	}
	eventID := fmt.Sprintf("payment.ap:%s:%s", itemID.String(), paymentRef)
	lines, err := s.settlementLines(ctx, "ap", amount, currency, docRate, payRate)
	if err != nil {
		return nil, nil, err
	}
	payment, err := s.repo.ApplyPaymentWithJournal(ctx, repository.PaymentWithJournalParams{
		EventID: eventID, EventType: "payment.disbursed", Source: "iag.finance", CorrelationID: paymentRef,
		Description: fmt.Sprintf("AP payment — %s", item.DocumentRef),
		Direction: "ap", OpenItemID: itemID, Amount: amount, Currency: currency, PaymentRef: paymentRef,
		FXRate: docRate, Lines: lines, Outbox: outbox,
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
