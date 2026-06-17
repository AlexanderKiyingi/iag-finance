package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ErrNoRate indicates no exchange rate is available to convert a non-base
// currency to the base currency on/before the requested date.
var ErrNoRate = errors.New("no exchange rate for currency")

// ExchangeRate is one currency→base conversion effective on a date.
type ExchangeRate struct {
	Currency     string    `json:"currency"`
	BaseCurrency string    `json:"baseCurrency"`
	Rate         string    `json:"rate"`
	AsOfDate     string    `json:"asOfDate"`
	CreatedAt    time.Time `json:"createdAt"`
}

// GetRate returns the conversion rate from currency to the base currency,
// effective on/before asOf. A base-currency (or empty) currency is always 1.
func (r *Repository) GetRate(ctx context.Context, currency string, asOf time.Time) (decimal.Decimal, error) {
	if currency == "" || currency == r.baseCurrency {
		return decimal.NewFromInt(1), nil
	}
	var rateStr string
	err := r.pool.QueryRow(ctx, `
		SELECT rate::text FROM exchange_rates
		WHERE currency = $1 AND base_currency = $2 AND as_of_date <= $3
		ORDER BY as_of_date DESC
		LIMIT 1
	`, currency, r.baseCurrency, asOf).Scan(&rateStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, ErrNoRate
		}
		return decimal.Zero, err
	}
	return decimal.NewFromString(rateStr)
}

// UpsertRate records (or updates) a currency→base rate for a date.
func (r *Repository) UpsertRate(ctx context.Context, currency, rate string, asOf time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO exchange_rates (currency, base_currency, rate, as_of_date)
		VALUES ($1, $2, $3::numeric, $4)
		ON CONFLICT (currency, base_currency, as_of_date)
		DO UPDATE SET rate = EXCLUDED.rate, created_at = NOW()
	`, currency, r.baseCurrency, rate, asOf)
	return err
}

// ListRates returns recent rates, newest first.
func (r *Repository) ListRates(ctx context.Context, limit int) ([]ExchangeRate, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT currency, base_currency, rate::text, as_of_date::text, created_at
		FROM exchange_rates
		ORDER BY as_of_date DESC, currency
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExchangeRate
	for rows.Next() {
		var e ExchangeRate
		if err := rows.Scan(&e.Currency, &e.BaseCurrency, &e.Rate, &e.AsOfDate, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ForeignBalance is one open AR/AP item's remaining foreign-currency balance and
// the rate it was booked at, for period-end revaluation.
type ForeignBalance struct {
	Direction string // ar|ap
	Currency  string
	Remaining decimal.Decimal
	DocRate   decimal.Decimal
}

// OpenForeignBalances returns the remaining balances of all open/partial AR & AP
// items denominated in a non-base currency.
func (r *Repository) OpenForeignBalances(ctx context.Context) ([]ForeignBalance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT 'ar', currency, (amount - amount_paid)::text, fx_rate::text
		FROM ar_open_items WHERE status IN ('open','partial') AND currency <> $1
		UNION ALL
		SELECT 'ap', currency, (amount - amount_paid)::text, fx_rate::text
		FROM ap_open_items WHERE status IN ('open','partial') AND currency <> $1
	`, r.baseCurrency)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForeignBalance
	for rows.Next() {
		var b ForeignBalance
		var remStr, rateStr string
		if err := rows.Scan(&b.Direction, &b.Currency, &remStr, &rateStr); err != nil {
			return nil, err
		}
		b.Remaining, _ = decimal.NewFromString(remStr)
		b.DocRate, _ = decimal.NewFromString(rateStr)
		out = append(out, b)
	}
	return out, rows.Err()
}

// RateOrOne returns the currency→base rate, defaulting to 1 when none is
// configured (so a missing rate degrades to 1:1 rather than blocking the write;
// operators must configure rates for correct FX reporting).
func (r *Repository) RateOrOne(ctx context.Context, currency string, asOf time.Time) decimal.Decimal {
	rate, err := r.GetRate(ctx, currency, asOf)
	if err != nil {
		return decimal.NewFromInt(1)
	}
	return rate
}

// OpenItemFXRate returns the per-document FX rate stored on an AR/AP open item,
// so that payments and adjustments for the document convert to base at the same
// rate the document was booked at (historical method → base stays balanced).
func (r *Repository) OpenItemFXRate(ctx context.Context, direction string, id uuid.UUID) (decimal.Decimal, error) {
	table := "ar_open_items"
	if direction == "ap" {
		table = "ap_open_items"
	}
	var rateStr string
	if err := r.pool.QueryRow(ctx, "SELECT fx_rate::text FROM "+table+" WHERE id = $1", id).Scan(&rateStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.NewFromInt(1), nil
		}
		return decimal.Zero, err
	}
	return decimal.NewFromString(rateStr)
}

// OpenItemFXRateByDocRef is OpenItemFXRate keyed by document_ref.
func (r *Repository) OpenItemFXRateByDocRef(ctx context.Context, direction, documentRef string) (decimal.Decimal, error) {
	table := "ar_open_items"
	if direction == "ap" {
		table = "ap_open_items"
	}
	var rateStr string
	if err := r.pool.QueryRow(ctx, "SELECT fx_rate::text FROM "+table+" WHERE document_ref = $1", documentRef).Scan(&rateStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.NewFromInt(1), nil
		}
		return decimal.Zero, err
	}
	return decimal.NewFromString(rateStr)
}
