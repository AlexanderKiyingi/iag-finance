package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// FixedAsset is one row of the fixed-asset subledger. NBV (cost − accumulated
// depreciation) is computed, not stored.
type FixedAsset struct {
	ID                      uuid.UUID       `json:"id"`
	AssetRef                string          `json:"assetRef"`
	Description             string          `json:"description"`
	Category                string          `json:"category"`
	Cost                    decimal.Decimal `json:"cost"`
	SalvageValue            decimal.Decimal `json:"salvageValue"`
	InServiceDate           string          `json:"inServiceDate"`
	UsefulLifeMonths        int             `json:"usefulLifeMonths"`
	Method                  string          `json:"method"`
	AccumulatedDepreciation decimal.Decimal `json:"accumulatedDepreciation"`
	NBV                     decimal.Decimal `json:"nbv"`
	Currency                string          `json:"currency"`
	Status                  string          `json:"status"`
	CreatedAt               time.Time       `json:"createdAt"`
}

type CreateFixedAssetInput struct {
	AssetRef         string
	Description      string
	Category         string
	Cost             decimal.Decimal
	SalvageValue     decimal.Decimal
	InServiceDate    time.Time
	UsefulLifeMonths int
	Currency         string
	// Method is 'straight_line' (default) or 'declining_balance'. DecliningRate
	// is the annual reducing-balance rate (e.g. 0.25), required for the latter.
	Method        string
	DecliningRate *decimal.Decimal
	// CapitalizeFromAccount, when set, posts the capitalization reclass
	// Dr 1500 Fixed Assets / Cr <this account> for Cost as of InServiceDate,
	// atomically with the asset row. Empty = record-only (no GL entry).
	CapitalizeFromAccount string
}

func (r *Repository) CreateFixedAsset(ctx context.Context, in CreateFixedAssetInput) (*FixedAsset, error) {
	currency := in.Currency
	if currency == "" {
		currency = r.baseCurrency
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	method := in.Method
	if method == "" {
		method = "straight_line"
	}
	var decliningRate *string
	if in.DecliningRate != nil {
		s := in.DecliningRate.String()
		decliningRate = &s
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO fa_assets (asset_ref, description, category, cost, salvage_value, in_service_date, useful_life_months, currency, method, declining_rate)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric)
		RETURNING id, asset_ref, description, category, cost::text, salvage_value::text, to_char(in_service_date,'YYYY-MM-DD'),
		          useful_life_months, method, accumulated_depreciation::text, currency, status, created_at
	`, in.AssetRef, in.Description, in.Category, in.Cost, in.SalvageValue, in.InServiceDate, in.UsefulLifeMonths, currency, method, decliningRate)
	asset, err := scanFixedAsset(row)
	if err != nil {
		return nil, err
	}

	// Capitalize: reclassify the cost from the expense account procurement booked
	// it to (default 5000) into Fixed Assets (1500), so the asset sits on the
	// balance sheet rather than in P&L. Same tx as the asset row.
	if from := in.CapitalizeFromAccount; from != "" && in.Cost.IsPositive() {
		fixedID, err := accountIDByCodeTx(ctx, tx, fixedAssetsCode)
		if err != nil {
			return nil, err
		}
		expenseID, err := accountIDByCodeTx(ctx, tx, from)
		if err != nil {
			return nil, err
		}
		entryNumber, err := nextEntryNumberTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		svc := "fa-capitalization"
		if _, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
			EntryNumber:    entryNumber,
			Description:    "Capitalize " + in.AssetRef,
			SourceService:  &svc,
			AccountingDate: in.InServiceDate,
			Currency:       currency,
			Lines: []ResolvedLine{
				{AccountID: fixedID, Debit: in.Cost, Memo: "Capitalize " + in.AssetRef, LineOrder: 0},
				{AccountID: expenseID, Credit: in.Cost, Memo: "Reclass from " + from, LineOrder: 1},
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

const fixedAssetsCode = "1500"

func (r *Repository) ListFixedAssets(ctx context.Context, limit, offset int) ([]FixedAsset, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, asset_ref, description, category, cost::text, salvage_value::text, to_char(in_service_date,'YYYY-MM-DD'),
		       useful_life_months, method, accumulated_depreciation::text, currency, status, created_at
		FROM fa_assets ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FixedAsset
	for rows.Next() {
		a, err := scanFixedAssetRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetFixedAssetByRef returns the subledger row for a warehouse asset tag, or nil
// when the asset was never capitalised in finance.
func (r *Repository) GetFixedAssetByRef(ctx context.Context, assetRef string) (*FixedAsset, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, asset_ref, description, category, cost::text, salvage_value::text, to_char(in_service_date,'YYYY-MM-DD'),
		       useful_life_months, method, accumulated_depreciation::text, currency, status, created_at
		FROM fa_assets WHERE asset_ref = $1
	`, assetRef)
	a, err := scanFixedAsset(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// MarkFixedAssetDisposedTx flips an asset to disposed inside tx (booking
// side-effect). No-op if already disposed or absent.
func MarkFixedAssetDisposedTx(ctx context.Context, tx pgx.Tx, assetRef string) error {
	_, err := tx.Exec(ctx, `
		UPDATE fa_assets SET status = 'disposed', updated_at = NOW()
		WHERE asset_ref = $1 AND status = 'active'`, assetRef)
	return err
}

// DepreciationRun summarises a period's posting.
type DepreciationRun struct {
	Period            string          `json:"period"`
	AssetsDepreciated int             `json:"assetsDepreciated"`
	TotalAmount       decimal.Decimal `json:"totalAmount"`
	JournalEntryID    *uuid.UUID      `json:"journalEntryId,omitempty"`
}

// RunDepreciation posts straight-line depreciation for the given 'YYYY-MM'
// period. It depreciates each active in-service asset that has no entry for the
// period yet (the (asset, period) unique index makes the run idempotent and
// incremental), capping accumulated depreciation at cost − salvage, then posts a
// single Dr Depreciation Expense / Cr Accumulated Depreciation journal for the
// period total — all in one transaction.
func (r *Repository) RunDepreciation(ctx context.Context, period string, postedAt time.Time) (*DepreciationRun, error) {
	periodEnd, err := lastDayOfPeriod(period)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT a.id, a.cost::text, a.salvage_value::text, a.useful_life_months, a.accumulated_depreciation::text,
		       a.method, COALESCE(a.declining_rate, 0)::text
		FROM fa_assets a
		LEFT JOIN fa_depreciation_entries e ON e.asset_id = a.id AND e.period = $1
		WHERE a.status = 'active' AND a.in_service_date <= $2 AND e.id IS NULL
		ORDER BY a.id
		FOR UPDATE OF a
	`, period, periodEnd)
	if err != nil {
		return nil, err
	}
	type assetRow struct {
		id                         uuid.UUID
		cost, salvage, accumulated decimal.Decimal
		life                       int
		method                     string
		decliningRate              decimal.Decimal
	}
	var assets []assetRow
	for rows.Next() {
		var ar assetRow
		var cost, salvage, accum, rate string
		if err := rows.Scan(&ar.id, &cost, &salvage, &ar.life, &accum, &ar.method, &rate); err != nil {
			rows.Close()
			return nil, err
		}
		ar.cost, _ = decimal.NewFromString(cost)
		ar.salvage, _ = decimal.NewFromString(salvage)
		ar.accumulated, _ = decimal.NewFromString(accum)
		ar.decliningRate, _ = decimal.NewFromString(rate)
		assets = append(assets, ar)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	total := decimal.Zero
	count := 0
	for _, a := range assets {
		depreciable := a.cost.Sub(a.salvage)
		remaining := depreciable.Sub(a.accumulated)
		if remaining.LessThanOrEqual(decimal.Zero) {
			continue
		}
		// Reducing-balance charges rate on net book value (cost − accumulated);
		// straight-line spreads the depreciable base evenly over the life.
		var monthly decimal.Decimal
		if a.method == "declining_balance" && a.decliningRate.IsPositive() {
			nbv := a.cost.Sub(a.accumulated)
			monthly = nbv.Mul(a.decliningRate).Div(decimal.NewFromInt(12)).Round(2)
		} else {
			monthly = depreciable.Div(decimal.NewFromInt(int64(a.life))).Round(2)
		}
		amount := monthly
		if amount.GreaterThan(remaining) {
			amount = remaining
		}
		if amount.LessThanOrEqual(decimal.Zero) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fa_depreciation_entries (asset_id, period, amount) VALUES ($1, $2, $3)
			ON CONFLICT (asset_id, period) DO NOTHING`, a.id, period, amount); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE fa_assets SET accumulated_depreciation = accumulated_depreciation + $2, updated_at = NOW()
			WHERE id = $1`, a.id, amount); err != nil {
			return nil, err
		}
		total = total.Add(amount)
		count++
	}

	run := &DepreciationRun{Period: period, AssetsDepreciated: count, TotalAmount: total}
	if total.IsPositive() {
		expenseID, err := accountIDByCodeTx(ctx, tx, "5300")
		if err != nil {
			return nil, err
		}
		accumID, err := accountIDByCodeTx(ctx, tx, "1510")
		if err != nil {
			return nil, err
		}
		entryNumber, err := nextEntryNumberTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		svc := "fa-depreciation"
		entryID, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
			EntryNumber:    entryNumber,
			Description:    "Depreciation " + period,
			SourceService:  &svc,
			AccountingDate: periodEnd,
			Lines: []ResolvedLine{
				{AccountID: expenseID, Debit: total, Memo: "Depreciation " + period, LineOrder: 0},
				{AccountID: accumID, Credit: total, Memo: "Accumulated depreciation " + period, LineOrder: 1},
			},
		}, postedAt)
		if err != nil {
			return nil, err
		}
		run.JournalEntryID = &entryID
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return run, nil
}

// BookAssetDisposalSubledger de-recognises a capitalised asset at its SYSTEM
// carrying amount and marks it disposed — all in ONE transaction with the
// fa_asset row locked FOR UPDATE, so a concurrent depreciation run can't make the
// accumulated depreciation stale (it blocks on the same row lock RunDepreciation
// takes). Books Dr Cash(proceeds) / Dr 1510 AccumDep / Cr 1500 cost / gain(4200)
// or loss(5200) on proceeds−NBV. Idempotent on eventID. Returns:
//   - (entry, true, nil)  booked (or the already-booked entry on redelivery)
//   - (nil, true, nil)    asset already disposed → no-op
//   - (nil, false, nil)   asset not in the subledger → caller falls back to the
//     hand-entered book value.
func (r *Repository) BookAssetDisposalSubledger(ctx context.Context, eventID, eventType, source, correlationID, currency, assetRef, description string, proceeds decimal.Decimal, audit *AuditInfo) (*domain.JournalEntry, bool, error) {
	if eventID != "" {
		processed, err := r.IsEventProcessed(ctx, eventID)
		if err != nil {
			return nil, false, err
		}
		if processed {
			e, err := r.GetJournalEntryBySourceEvent(ctx, eventID)
			return e, true, err
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var costS, accumS, status string
	err = tx.QueryRow(ctx, `
		SELECT cost::text, accumulated_depreciation::text, status
		FROM fa_assets WHERE asset_ref = $1 FOR UPDATE`, assetRef,
	).Scan(&costS, &accumS, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil // not capitalised → caller falls back
	}
	if err != nil {
		return nil, false, err
	}
	if status != "active" {
		return nil, true, nil // already disposed → no-op
	}
	cost, _ := decimal.NewFromString(costS)
	accumulated, _ := decimal.NewFromString(accumS)
	nbv := cost.Sub(accumulated)

	lines := make([]ResolvedLine, 0, 4)
	order := 0
	add := func(code string, debit, credit decimal.Decimal, memo string) error {
		id, err := accountIDByCodeTx(ctx, tx, code)
		if err != nil {
			return err
		}
		lines = append(lines, ResolvedLine{AccountID: id, Debit: debit, Credit: credit, Memo: memo, LineOrder: order})
		order++
		return nil
	}
	if proceeds.IsPositive() {
		if err := add("1000", proceeds, decimal.Zero, "Disposal proceeds "+assetRef); err != nil {
			return nil, false, err
		}
	}
	if accumulated.IsPositive() {
		if err := add("1510", accumulated, decimal.Zero, "Reverse accumulated depreciation "+assetRef); err != nil {
			return nil, false, err
		}
	}
	if cost.IsPositive() {
		if err := add("1500", decimal.Zero, cost, "De-recognise asset cost "+assetRef); err != nil {
			return nil, false, err
		}
	}
	net := proceeds.Sub(nbv)
	switch {
	case net.IsPositive():
		if err := add("4200", decimal.Zero, net, "Gain on disposal"); err != nil {
			return nil, false, err
		}
	case net.IsNegative():
		if err := add("5200", net.Neg(), decimal.Zero, "Loss on disposal"); err != nil {
			return nil, false, err
		}
	}

	entryNumber, err := nextEntryNumberTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	var srcEvent, srcSvc, corr *string
	if eventID != "" {
		srcEvent = &eventID
	}
	if source != "" {
		srcSvc = &source
	}
	if correlationID != "" {
		corr = &correlationID
	}
	entryID, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
		EntryNumber:   entryNumber,
		Description:   description,
		SourceEventID: srcEvent,
		SourceService: srcSvc,
		CorrelationID: corr,
		Currency:      currency,
		Lines:         lines,
	}, time.Now().UTC())
	if err != nil {
		if eventID != "" && IsUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			e, err := r.GetJournalEntryBySourceEvent(ctx, eventID)
			return e, true, err
		}
		return nil, false, err
	}
	if eventID != "" {
		won, err := markProcessedTx(ctx, tx, eventID, eventType)
		if err != nil {
			return nil, false, err
		}
		if !won {
			_ = tx.Rollback(ctx)
			e, err := r.GetJournalEntryBySourceEvent(ctx, eventID)
			return e, true, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE fa_assets SET status = 'disposed', updated_at = NOW() WHERE asset_ref = $1`, assetRef); err != nil {
		return nil, false, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	e, err := r.GetJournalEntry(ctx, entryID)
	return e, true, err
}

func accountIDByCodeTx(ctx context.Context, tx pgx.Tx, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM chart_of_accounts WHERE code = $1 AND active = TRUE`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("account %s not found", code)
	}
	return id, err
}

func lastDayOfPeriod(period string) (time.Time, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, fmt.Errorf("period must be YYYY-MM: %w", err)
	}
	return t.AddDate(0, 1, -1), nil
}

func scanFixedAsset(row scannable) (*FixedAsset, error) {
	var a FixedAsset
	var cost, salvage, accum string
	if err := row.Scan(&a.ID, &a.AssetRef, &a.Description, &a.Category, &cost, &salvage, &a.InServiceDate,
		&a.UsefulLifeMonths, &a.Method, &accum, &a.Currency, &a.Status, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.Cost, _ = decimal.NewFromString(cost)
	a.SalvageValue, _ = decimal.NewFromString(salvage)
	a.AccumulatedDepreciation, _ = decimal.NewFromString(accum)
	a.NBV = a.Cost.Sub(a.AccumulatedDepreciation)
	return &a, nil
}

func scanFixedAssetRows(rows pgx.Rows) (*FixedAsset, error) { return scanFixedAsset(rows) }
