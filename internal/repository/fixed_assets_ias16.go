package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// IAS 36 impairment and IAS 16 revaluation for the fixed-asset subledger.
//
// Impairment reduces net book value via accumulated depreciation (Dr 5310 / Cr
// 1510) and can be reversed. Revaluation adjusts gross cost by the movement to
// the new carrying amount: upward credits Revaluation Surplus (3100, equity);
// downward consumes prior surplus first, then hits Impairment Loss (5310).

const (
	impairmentLossCode = "5310"
	revalSurplusCode   = "3100"
	accumDepCode       = "1510"
)

// ErrAssetNotFound indicates the asset is not in the finance subledger.
var ErrAssetNotFound = errors.New("fixed asset not found in subledger")

// ErrAssetInactive indicates the asset is disposed and cannot be adjusted.
var ErrAssetInactive = errors.New("fixed asset is not active")

// ErrNoImpairment indicates there is nothing to impair (recoverable ≥ NBV).
var ErrNoImpairment = errors.New("recoverable amount is not below carrying amount")

// BookImpairment writes down an asset to its recoverable amount (IAS 36),
// booking Dr Impairment Loss / Cr Accumulated Depreciation and increasing
// accumulated depreciation. Idempotent on fa.impair:<ref>:<date>.
func (r *Repository) BookImpairment(ctx context.Context, assetRef string, recoverable decimal.Decimal, effective time.Time, audit *AuditInfo) (*FixedAsset, error) {
	eventID := "fa.impair:" + assetRef + ":" + effective.Format("2006-01-02")
	if processed, err := r.IsEventProcessed(ctx, eventID); err != nil {
		return nil, err
	} else if processed {
		return r.GetFixedAssetByRef(ctx, assetRef)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cost, accumulated, status, assetID, err := lockAsset(ctx, tx, assetRef)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, ErrAssetInactive
	}
	nbv := cost.Sub(accumulated)
	loss := nbv.Sub(recoverable)
	if loss.LessThanOrEqual(decimal.Zero) {
		return nil, ErrNoImpairment
	}

	lines, err := resolveTxLines(ctx, tx, []codeLine{
		{impairmentLossCode, loss, decimal.Zero, "Impairment loss " + assetRef},
		{accumDepCode, decimal.Zero, loss, "Impairment write-down " + assetRef},
	})
	if err != nil {
		return nil, err
	}
	if err := postAssetAdjustment(ctx, tx, r, eventID, "finance.asset.impair", "Impairment "+assetRef, effective, lines); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE fa_assets SET accumulated_depreciation = accumulated_depreciation + $2::numeric, updated_at = NOW() WHERE id = $1`, assetID, loss.String()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO fa_impairments (asset_id, effective_date, recoverable_amount, impairment_loss, is_reversal)
		VALUES ($1, $2, $3::numeric, $4::numeric, FALSE)
	`, assetID, effective, recoverable.String(), loss.String()); err != nil {
		return nil, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetFixedAssetByRef(ctx, assetRef)
}

// ReverseImpairment reverses a prior impairment by amount (IAS 36), booking
// Dr Accumulated Depreciation / Cr Impairment Loss, capped so accumulated
// depreciation cannot go negative. Idempotent on fa.impair.rev:<ref>:<date>.
func (r *Repository) ReverseImpairment(ctx context.Context, assetRef string, amount decimal.Decimal, effective time.Time, audit *AuditInfo) (*FixedAsset, error) {
	eventID := "fa.impair.rev:" + assetRef + ":" + effective.Format("2006-01-02")
	if processed, err := r.IsEventProcessed(ctx, eventID); err != nil {
		return nil, err
	} else if processed {
		return r.GetFixedAssetByRef(ctx, assetRef)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	_, accumulated, status, assetID, err := lockAsset(ctx, tx, assetRef)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, ErrAssetInactive
	}
	if amount.GreaterThan(accumulated) {
		amount = accumulated
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrNoImpairment
	}

	lines, err := resolveTxLines(ctx, tx, []codeLine{
		{accumDepCode, amount, decimal.Zero, "Impairment reversal " + assetRef},
		{impairmentLossCode, decimal.Zero, amount, "Impairment reversal income " + assetRef},
	})
	if err != nil {
		return nil, err
	}
	if err := postAssetAdjustment(ctx, tx, r, eventID, "finance.asset.impair.reverse", "Impairment reversal "+assetRef, effective, lines); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE fa_assets SET accumulated_depreciation = accumulated_depreciation - $2::numeric, updated_at = NOW() WHERE id = $1`, assetID, amount.String()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO fa_impairments (asset_id, effective_date, impairment_loss, is_reversal)
		VALUES ($1, $2, $3::numeric, TRUE)
	`, assetID, effective, amount.Neg().String()); err != nil {
		return nil, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetFixedAssetByRef(ctx, assetRef)
}

// BookRevaluation restates an asset to newCarrying (IAS 16 revaluation model) by
// adjusting gross cost by the movement. Upward credits Revaluation Surplus;
// downward consumes prior surplus, then Impairment Loss. Idempotent on
// fa.revalue:<ref>:<date>.
func (r *Repository) BookRevaluation(ctx context.Context, assetRef string, newCarrying decimal.Decimal, effective time.Time, audit *AuditInfo) (*FixedAsset, error) {
	eventID := "fa.revalue:" + assetRef + ":" + effective.Format("2006-01-02")
	if processed, err := r.IsEventProcessed(ctx, eventID); err != nil {
		return nil, err
	} else if processed {
		return r.GetFixedAssetByRef(ctx, assetRef)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cost, accumulated, status, assetID, err := lockAsset(ctx, tx, assetRef)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, ErrAssetInactive
	}
	nbv := cost.Sub(accumulated)
	delta := newCarrying.Sub(nbv)
	if delta.IsZero() {
		return nil, ErrNoImpairment
	}

	var lines []ResolvedLine
	if delta.IsPositive() {
		// Upward: Dr Fixed Assets / Cr Revaluation Surplus.
		lines, err = resolveTxLines(ctx, tx, []codeLine{
			{fixedAssetsCode, delta, decimal.Zero, "Revaluation increase " + assetRef},
			{revalSurplusCode, decimal.Zero, delta, "Revaluation surplus " + assetRef},
		})
	} else {
		down := delta.Neg()
		// Downward: consume prior net surplus for this asset first, remainder to P&L.
		var surplusS string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(surplus_delta),0)::text FROM fa_revaluations WHERE asset_id = $1`, assetID).Scan(&surplusS); err != nil {
			return nil, err
		}
		surplus, _ := decimal.NewFromString(surplusS)
		if surplus.IsNegative() {
			surplus = decimal.Zero
		}
		fromSurplus := down
		if surplus.LessThan(fromSurplus) {
			fromSurplus = surplus
		}
		toExpense := down.Sub(fromSurplus)
		cl := []codeLine{}
		if fromSurplus.IsPositive() {
			cl = append(cl, codeLine{revalSurplusCode, fromSurplus, decimal.Zero, "Revaluation surplus release " + assetRef})
		}
		if toExpense.IsPositive() {
			cl = append(cl, codeLine{impairmentLossCode, toExpense, decimal.Zero, "Revaluation decrease " + assetRef})
		}
		cl = append(cl, codeLine{fixedAssetsCode, decimal.Zero, down, "Revaluation decrease " + assetRef})
		lines, err = resolveTxLines(ctx, tx, cl)
	}
	if err != nil {
		return nil, err
	}
	if err := postAssetAdjustment(ctx, tx, r, eventID, "finance.asset.revalue", "Revaluation "+assetRef, effective, lines); err != nil {
		return nil, err
	}
	// Adjust gross cost by the movement so NBV becomes newCarrying.
	if _, err := tx.Exec(ctx, `UPDATE fa_assets SET cost = cost + $2::numeric, updated_at = NOW() WHERE id = $1`, assetID, delta.String()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO fa_revaluations (asset_id, effective_date, new_carrying_amount, surplus_delta)
		VALUES ($1, $2, $3::numeric, $4::numeric)
	`, assetID, effective, newCarrying.String(), delta.String()); err != nil {
		return nil, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetFixedAssetByRef(ctx, assetRef)
}

// lockAsset row-locks a fixed asset and returns cost, accumulated, status, id.
func lockAsset(ctx context.Context, tx pgx.Tx, assetRef string) (decimal.Decimal, decimal.Decimal, string, uuid.UUID, error) {
	var costS, accumS, status string
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id, cost::text, accumulated_depreciation::text, status
		FROM fa_assets WHERE asset_ref = $1 FOR UPDATE
	`, assetRef).Scan(&id, &costS, &accumS, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return decimal.Zero, decimal.Zero, "", uuid.Nil, ErrAssetNotFound
	}
	if err != nil {
		return decimal.Zero, decimal.Zero, "", uuid.Nil, err
	}
	cost, _ := decimal.NewFromString(costS)
	accum, _ := decimal.NewFromString(accumS)
	return cost, accum, status, id, nil
}

// codeLine is an unresolved GL line by account code.
type codeLine struct {
	Code   string
	Debit  decimal.Decimal
	Credit decimal.Decimal
	Memo   string
}

// resolveTxLines resolves code lines to ResolvedLine within tx.
func resolveTxLines(ctx context.Context, tx pgx.Tx, lines []codeLine) ([]ResolvedLine, error) {
	out := make([]ResolvedLine, 0, len(lines))
	for i, l := range lines {
		id, err := accountIDByCodeTx(ctx, tx, l.Code)
		if err != nil {
			return nil, err
		}
		out = append(out, ResolvedLine{AccountID: id, Debit: l.Debit, Credit: l.Credit, Memo: l.Memo, LineOrder: i})
	}
	return out, nil
}

// postAssetAdjustment inserts a posted entry and marks the source event, inside
// an existing tx (the asset row is already locked by the caller).
func postAssetAdjustment(ctx context.Context, tx pgx.Tx, r *Repository, eventID, eventType, description string, effective time.Time, lines []ResolvedLine) error {
	entryNumber, err := nextEntryNumberTx(ctx, tx)
	if err != nil {
		return err
	}
	src := "iag.finance"
	if _, err := r.insertPostedEntryTx(ctx, tx, CreateJournalParams{
		EntryNumber:    entryNumber,
		Description:    description,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: effective,
		Lines:          lines,
	}, effective); err != nil {
		return err
	}
	won, err := markProcessedTx(ctx, tx, eventID, eventType)
	if err != nil {
		return err
	}
	if !won {
		return ErrDuplicateEventRepo
	}
	return nil
}

// ErrDuplicateEventRepo is returned when a source event was already processed
// mid-transaction (a concurrent racer won).
var ErrDuplicateEventRepo = errors.New("source event already processed")
