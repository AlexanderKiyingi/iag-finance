package ledger

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/payments"
	"github.com/iag-finance/backend/internal/repository"
)

// CreatePaymentIntent records an intent to collect `amount` on an AR open item
// via the manual provider (a real gateway plugs in behind payments.Gateway).
func (s *Service) CreatePaymentIntent(ctx context.Context, openItemID uuid.UUID, amount decimal.Decimal, currency string) (*repository.PaymentIntent, error) {
	item, err := s.repo.GetAROpenItem(ctx, openItemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrOpenItemNotFound
	}
	if currency == "" {
		currency = item.Currency
	}
	gw := payments.Manual{}
	res, err := gw.CreateIntent(ctx, payments.IntentRequest{
		IntentID: uuid.NewString(), OpenItemID: openItemID.String(),
		Amount: amount.String(), Currency: currency, CustomerRef: item.CustomerRef,
	})
	if err != nil {
		return nil, err
	}
	return s.repo.CreatePaymentIntent(ctx, openItemID, amount.String(), currency, gw.Name(), res.ExternalRef, res.CheckoutURL)
}

// ConfirmPaymentIntent settles a pending intent against its open item (reusing
// the AR payment path) and marks it succeeded. Idempotent on the intent id.
func (s *Service) ConfirmPaymentIntent(ctx context.Context, id uuid.UUID, actor string) (*repository.PaymentIntent, error) {
	pi, err := s.repo.GetPaymentIntent(ctx, id)
	if err != nil {
		return nil, err
	}
	if pi == nil {
		return nil, repository.ErrPaymentIntentNotFound
	}
	if pi.Status == "succeeded" {
		return pi, nil
	}
	amount, _ := decimal.NewFromString(pi.Amount)
	if _, _, err := s.ApplyARPayment(ctx, pi.OpenItemID, amount, pi.Currency, "intent:"+id.String(), actor, nil); err != nil {
		_ = s.repo.MarkPaymentIntentStatus(ctx, id, "failed")
		return nil, err
	}
	if err := s.repo.MarkPaymentIntentStatus(ctx, id, "succeeded"); err != nil {
		return nil, err
	}
	return s.repo.GetPaymentIntent(ctx, id)
}
