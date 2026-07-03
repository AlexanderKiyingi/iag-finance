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

// AddGRNIAccrualTx raises the accrued total (and received quantity) for a PO
// inside tx — called as a booking side-effect when goods are received, so the
// accrual and its GL entry commit atomically. qtyReceived may be zero when the
// goods-receipt event carries no quantity.
func AddGRNIAccrualTx(ctx context.Context, tx pgx.Tx, poRef, currency string, amount, qtyReceived decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO grni_accruals (po_ref, currency, accrued, qty_received)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (po_ref) DO UPDATE
		SET accrued = grni_accruals.accrued + EXCLUDED.accrued,
		    qty_received = grni_accruals.qty_received + EXCLUDED.qty_received,
		    updated_at = NOW()
	`, poRef, currency, amount, qtyReceived)
	return err
}

// ClearGRNIAccrualTx raises the cleared total (and invoiced quantity) for a PO
// inside tx — called as a booking side-effect when the matching invoice routes
// its net through GR/IR. Upserts so an invoice that arrives before its goods
// receipt still records the clearing (accrued is then added when the GRN lands).
func ClearGRNIAccrualTx(ctx context.Context, tx pgx.Tx, poRef, currency string, amount, qtyInvoiced decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO grni_accruals (po_ref, currency, cleared, qty_invoiced)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (po_ref) DO UPDATE
		SET cleared = grni_accruals.cleared + EXCLUDED.cleared,
		    qty_invoiced = grni_accruals.qty_invoiced + EXCLUDED.qty_invoiced,
		    updated_at = NOW()
	`, poRef, currency, amount, qtyInvoiced)
	return err
}
