package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrPaymentIntentNotFound = errors.New("payment intent not found")

type PaymentIntent struct {
	ID          uuid.UUID `json:"id"`
	OpenItemID  uuid.UUID `json:"openItemId"`
	Amount      string    `json:"amount"`
	Currency    string    `json:"currency"`
	Provider    string    `json:"provider"`
	Status      string    `json:"status"`
	ExternalRef *string   `json:"externalRef,omitempty"`
	CheckoutURL *string   `json:"checkoutUrl,omitempty"`
}

func (r *Repository) CreatePaymentIntent(ctx context.Context, openItemID uuid.UUID, amount, currency, provider, externalRef, checkoutURL string) (*PaymentIntent, error) {
	var pi PaymentIntent
	err := r.pool.QueryRow(ctx, `
		INSERT INTO payment_intents (entity_id, open_item_id, amount, currency, provider, external_ref, checkout_url)
		VALUES ($1, $2, $3::numeric, $4, $5, NULLIF($6,''), NULLIF($7,''))
		RETURNING id, open_item_id, amount::text, currency, provider, status, external_ref, checkout_url
	`, EntityFromContext(ctx), openItemID, amount, currency, provider, externalRef, checkoutURL).Scan(
		&pi.ID, &pi.OpenItemID, &pi.Amount, &pi.Currency, &pi.Provider, &pi.Status, &pi.ExternalRef, &pi.CheckoutURL)
	if err != nil {
		return nil, err
	}
	return &pi, nil
}

func (r *Repository) GetPaymentIntent(ctx context.Context, id uuid.UUID) (*PaymentIntent, error) {
	var pi PaymentIntent
	err := r.pool.QueryRow(ctx, `
		SELECT id, open_item_id, amount::text, currency, provider, status, external_ref, checkout_url
		FROM payment_intents WHERE id = $1
	`, id).Scan(&pi.ID, &pi.OpenItemID, &pi.Amount, &pi.Currency, &pi.Provider, &pi.Status, &pi.ExternalRef, &pi.CheckoutURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &pi, nil
}

func (r *Repository) MarkPaymentIntentStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE payment_intents SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}
