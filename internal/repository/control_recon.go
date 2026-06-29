package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ControlReconRow ties one subledger control account in the general ledger to
// the sum of its open items, so a drift (GL ≠ subledger) is surfaced.
type ControlReconRow struct {
	Control     string `json:"control"`     // "Accounts Receivable" / "Accounts Payable"
	AccountCode string `json:"accountCode"` // 1100 / 2000
	GLBalance   string `json:"glBalance"`   // base-currency GL balance of the control account
	Subledger   string `json:"subledger"`   // sum of open subledger item balances
	Difference  string `json:"difference"`  // glBalance − subledger (0 = reconciled)
}

// ControlReconciliation compares each subledger control account's GL balance to
// the sum of its open items. AR control is account 1100 (debit-normal), AP
// control is 2000 (credit-normal). The GL side is base currency; the subledger
// side sums document-currency balances, so the two tie exactly on a
// single-currency ledger (a non-zero difference under multi-currency reflects
// unrevalued FX, which the FX revaluation run addresses).
func (r *Repository) ControlReconciliation(ctx context.Context, entityIDs []uuid.UUID) ([]ControlReconRow, error) {
	glBalance := func(code, normal string) (decimal.Decimal, error) {
		expr := "jl.debit_base - jl.credit_base" // debit-normal (assets)
		if normal == "credit" {
			expr = "jl.credit_base - jl.debit_base" // credit-normal (liabilities)
		}
		var s string
		err := r.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(`+expr+`) FILTER (WHERE je.id IS NOT NULL), 0)::text
			FROM chart_of_accounts coa
			LEFT JOIN journal_lines jl ON jl.account_id = coa.id
			LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
				AND je.entity_id = ANY($2::uuid[])
			WHERE coa.code = $1
		`, code, entityIDs).Scan(&s)
		if err != nil {
			return decimal.Zero, err
		}
		return decimal.NewFromString(s)
	}

	subBalance := func(table string) (decimal.Decimal, error) {
		// table is a package constant ('ar_open_items'/'ap_open_items'), never user input.
		var s string
		err := r.pool.QueryRow(ctx,
			"SELECT COALESCE(SUM(amount - amount_paid), 0)::text FROM "+table+
				" WHERE status <> 'closed' AND entity_id = ANY($1::uuid[])", entityIDs).Scan(&s)
		if err != nil {
			return decimal.Zero, err
		}
		return decimal.NewFromString(s)
	}

	arGL, err := glBalance("1100", "debit")
	if err != nil {
		return nil, err
	}
	apGL, err := glBalance("2000", "credit")
	if err != nil {
		return nil, err
	}
	arSub, err := subBalance("ar_open_items")
	if err != nil {
		return nil, err
	}
	apSub, err := subBalance("ap_open_items")
	if err != nil {
		return nil, err
	}

	mk := func(name, code string, gl, sub decimal.Decimal) ControlReconRow {
		return ControlReconRow{
			Control:     name,
			AccountCode: code,
			GLBalance:   gl.StringFixed(2),
			Subledger:   sub.StringFixed(2),
			Difference:  gl.Sub(sub).StringFixed(2),
		}
	}
	return []ControlReconRow{
		mk("Accounts Receivable", "1100", arGL, arSub),
		mk("Accounts Payable", "2000", apGL, apSub),
	}, nil
}
