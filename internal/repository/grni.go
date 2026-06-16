package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// GRNIAccrualOpen returns the un-cleared GR/IR accrual (accrued - cleared) for a
// PO and its currency. Zero / "" when no accrual exists for the PO.
func (r *Repository) GRNIAccrualOpen(ctx context.Context, poRef string) (decimal.Decimal, string, error) {
	var openStr, currency string
	err := r.pool.QueryRow(ctx, `
		SELECT (accrued - cleared)::text, currency FROM grni_accruals WHERE po_ref = $1
	`, poRef).Scan(&openStr, &currency)
	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, "", nil
		}
		return decimal.Zero, "", err
	}
	d, err := decimal.NewFromString(openStr)
	return d, currency, err
}

// AddGRNIAccrualTx raises the accrued total for a PO inside tx — called as a
// booking side-effect when goods are received, so the accrual and its GL entry
// commit atomically.
func AddGRNIAccrualTx(ctx context.Context, tx pgx.Tx, poRef, currency string, amount decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO grni_accruals (po_ref, currency, accrued)
		VALUES ($1, $2, $3)
		ON CONFLICT (po_ref) DO UPDATE
		SET accrued = grni_accruals.accrued + EXCLUDED.accrued, updated_at = NOW()
	`, poRef, currency, amount)
	return err
}

// ClearGRNIAccrualTx raises the cleared total for a PO inside tx — called as a
// booking side-effect when the matching invoice routes its net through GR/IR.
// Upserts so an invoice that arrives before its goods receipt still records the
// clearing (accrued is then added when the GRN lands).
func ClearGRNIAccrualTx(ctx context.Context, tx pgx.Tx, poRef, currency string, amount decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO grni_accruals (po_ref, currency, cleared)
		VALUES ($1, $2, $3)
		ON CONFLICT (po_ref) DO UPDATE
		SET cleared = grni_accruals.cleared + EXCLUDED.cleared, updated_at = NOW()
	`, poRef, currency, amount)
	return err
}
