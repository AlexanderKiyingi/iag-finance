package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// FXConversion is a recorded treasury currency conversion between bank accounts.
type FXConversion struct {
	ID              string    `json:"id"`
	ConversionRef   string    `json:"conversionRef"`
	FromAccount     string    `json:"fromAccount"`
	FromCurrency    string    `json:"fromCurrency"`
	FromAmount      string    `json:"fromAmount"`
	ToAccount       string    `json:"toAccount"`
	ToCurrency      string    `json:"toCurrency"`
	ExchangeRate    string    `json:"exchangeRate"`
	ConvertedAmount string    `json:"convertedAmount"`
	Fees            string    `json:"fees"`
	GainLossAccount string    `json:"gainLossAccount"`
	ConversionDate  string    `json:"conversionDate"`
	Notes           string    `json:"notes"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type CreateFXConversionInput struct {
	ConversionRef   string
	FromAccount     string
	FromCurrency    string
	FromAmount      decimal.Decimal
	ToAccount       string
	ToCurrency      string
	ExchangeRate    decimal.Decimal
	ConvertedAmount decimal.Decimal
	Fees            decimal.Decimal
	GainLossAccount string
	ConversionDate  time.Time
	Notes           string
}

func (r *Repository) CreateFXConversion(ctx context.Context, in CreateFXConversionInput) (*FXConversion, error) {
	ref := in.ConversionRef
	if ref == "" {
		var max *int
		_ = r.pool.QueryRow(ctx, `SELECT MAX(CAST(SUBSTRING(conversion_ref FROM 9) AS INT)) FROM fx_conversions WHERE conversion_ref LIKE 'FX-2026-%'`).Scan(&max)
		n := 1
		if max != nil {
			n = *max + 1
		}
		ref = fmt.Sprintf("FX-2026-%03d", n)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO fx_conversions (conversion_ref, from_account, from_currency, from_amount, to_account, to_currency,
		                            exchange_rate, converted_amount, fees, gain_loss_account, conversion_date, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id::text, conversion_ref, from_account, from_currency, from_amount::text, to_account, to_currency,
		          exchange_rate::text, converted_amount::text, fees::text, gain_loss_account,
		          to_char(conversion_date,'YYYY-MM-DD'), notes, status, created_at
	`, ref, in.FromAccount, in.FromCurrency, in.FromAmount, in.ToAccount, in.ToCurrency,
		in.ExchangeRate, in.ConvertedAmount, in.Fees, in.GainLossAccount, in.ConversionDate, in.Notes)
	return scanFXConversion(row)
}

func (r *Repository) ListFXConversions(ctx context.Context, limit, offset int) ([]FXConversion, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, conversion_ref, from_account, from_currency, from_amount::text, to_account, to_currency,
		       exchange_rate::text, converted_amount::text, fees::text, gain_loss_account,
		       to_char(conversion_date,'YYYY-MM-DD'), notes, status, created_at
		FROM fx_conversions ORDER BY conversion_date DESC, created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FXConversion
	for rows.Next() {
		e, err := scanFXConversion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func scanFXConversion(s scannable) (*FXConversion, error) {
	var f FXConversion
	if err := s.Scan(&f.ID, &f.ConversionRef, &f.FromAccount, &f.FromCurrency, &f.FromAmount, &f.ToAccount,
		&f.ToCurrency, &f.ExchangeRate, &f.ConvertedAmount, &f.Fees, &f.GainLossAccount, &f.ConversionDate,
		&f.Notes, &f.Status, &f.CreatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}
