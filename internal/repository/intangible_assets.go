package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// IntangibleAsset is an IAS 38 intangible in the finance subledger.
type IntangibleAsset struct {
	ID                      string    `json:"id"`
	AssetRef                string    `json:"assetRef"`
	Description             string    `json:"description"`
	Category                string    `json:"category"`
	Cost                    string    `json:"cost"`
	InServiceDate           string    `json:"inServiceDate"`
	UsefulLifeMonths        int       `json:"usefulLifeMonths"`
	AccumulatedAmortization string    `json:"accumulatedAmortization"`
	Currency                string    `json:"currency"`
	Status                  string    `json:"status"`
	CreatedAt               time.Time `json:"createdAt"`
}

type CreateIntangibleAssetInput struct {
	AssetRef         string
	Description      string
	Category         string
	Cost             decimal.Decimal
	InServiceDate    time.Time
	UsefulLifeMonths int
	Currency         string
	// CapitalizeFromAccount, when set, posts Dr 1700 Intangible Assets /
	// Cr <this account> for Cost as of InServiceDate, atomically with the row.
	// Empty = record-only (no GL entry).
	CapitalizeFromAccount string
}

const intangibleAssetsCode = "1700"

func (r *Repository) CreateIntangibleAsset(ctx context.Context, in CreateIntangibleAssetInput) (*IntangibleAsset, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO ia_assets (asset_ref, description, category, cost, in_service_date, useful_life_months, currency)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, asset_ref, description, category, cost::text, to_char(in_service_date,'YYYY-MM-DD'),
		          useful_life_months, accumulated_amortization::text, currency, status, created_at
	`, in.AssetRef, in.Description, in.Category, in.Cost, in.InServiceDate, in.UsefulLifeMonths, currency)
	asset, err := scanIntangibleAsset(row)
	if err != nil {
		return nil, err
	}

	// Capitalize: reclassify the cost from the expense account procurement booked
	// it to (default 5000) into Intangible Assets (1700). Same tx as the row.
	if from := in.CapitalizeFromAccount; from != "" && in.Cost.IsPositive() {
		iaID, err := accountIDByCodeTx(ctx, tx, intangibleAssetsCode)
		if err != nil {
			return nil, err
		}
		fromID, err := accountIDByCodeTx(ctx, tx, from)
		if err != nil {
			return nil, err
		}
		entryNumber, err := nextEntryNumberTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		svc := "ia-capitalization"
		if _, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
			EntryNumber:    entryNumber,
			Description:    "Capitalize " + in.AssetRef,
			SourceService:  &svc,
			AccountingDate: in.InServiceDate,
			Currency:       currency,
			Lines: []ResolvedLine{
				{AccountID: iaID, Debit: in.Cost, Memo: "Capitalize " + in.AssetRef, LineOrder: 0},
				{AccountID: fromID, Credit: in.Cost, Memo: "Reclass from " + from, LineOrder: 1},
			},
		}, in.InServiceDate); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return asset, nil
}

func (r *Repository) ListIntangibleAssets(ctx context.Context, limit, offset int) ([]IntangibleAsset, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, asset_ref, description, category, cost::text, to_char(in_service_date,'YYYY-MM-DD'),
		       useful_life_months, accumulated_amortization::text, currency, status, created_at
		FROM ia_assets ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntangibleAsset
	for rows.Next() {
		a, err := scanIntangibleAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// scanIntangibleAsset scans a row/rows (both satisfy the shared `scannable`
// interface used elsewhere in this package).
func scanIntangibleAsset(s scannable) (*IntangibleAsset, error) {
	var a IntangibleAsset
	if err := s.Scan(&a.ID, &a.AssetRef, &a.Description, &a.Category, &a.Cost, &a.InServiceDate,
		&a.UsefulLifeMonths, &a.AccumulatedAmortization, &a.Currency, &a.Status, &a.CreatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}
