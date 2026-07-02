package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SalesByItemRow aggregates issued-invoice line items by their description
// ("item"), giving quantity sold, net revenue and tax over a window. Base
// currency via line_total/tax_amount (already net of the invoice's own currency
// at issue). Manager's "Sales Invoice Totals by Item" equivalent.
type SalesByItemRow struct {
	Item     string `json:"item"`
	Quantity string `json:"quantity"`
	Revenue  string `json:"revenue"`
	Tax      string `json:"tax"`
}

func (r *Repository) SalesByItem(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]SalesByItemRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(TRIM(il.description), ''), '(unlabelled)') AS item,
			SUM(il.quantity)::text,
			SUM(il.line_total)::text,
			SUM(il.tax_amount)::text
		FROM invoice_lines il
		JOIN invoices inv ON inv.id = il.invoice_id
			AND inv.status IN ('issued', 'paid')
			AND inv.entity_id = ANY($1::uuid[])
			AND ($2::date IS NULL OR inv.issue_date >= $2)
			AND ($3::date IS NULL OR inv.issue_date <= $3)
		GROUP BY 1
		ORDER BY SUM(il.line_total) DESC
	`, entityIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesByItemRow
	for rows.Next() {
		var row SalesByItemRow
		if err := rows.Scan(&row.Item, &row.Quantity, &row.Revenue, &row.Tax); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// EquityChangeRow is one equity account's movement over a window: its balance at
// the opening (day before `from`, or all-time when `from` is nil) and at the
// close (`to`). The report also carries the period's net income as the movement
// in retained earnings. Manager's "Statement of Changes in Equity".
type EquityChangeRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Opening     string `json:"opening"`
	Movement    string `json:"movement"`
	Closing     string `json:"closing"`
}

// StatementOfChangesInEquity returns per-equity-account opening/closing balances
// (credit-normal) plus a synthetic "Current Period Earnings" row equal to net
// income for [from, to], so opening equity + movements = closing equity.
func (r *Repository) StatementOfChangesInEquity(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]EquityChangeRow, error) {
	// opening = posted equity movement strictly before `from`; closing = through `to`.
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name,
			SUM(CASE WHEN ($2::date IS NULL OR je.accounting_date < $2)
				THEN jl.credit_base - jl.debit_base ELSE 0 END)::text AS opening,
			SUM(CASE WHEN ($3::date IS NULL OR je.accounting_date <= $3)
				THEN jl.credit_base - jl.debit_base ELSE 0 END)::text AS closing
		FROM chart_of_accounts coa
		JOIN journal_lines jl ON jl.account_id = coa.id
		JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND je.entity_id = ANY($1::uuid[])
		WHERE coa.active = TRUE AND coa.account_type = 'equity'
		GROUP BY coa.id, coa.code, coa.name
		HAVING SUM(jl.debit_base) > 0 OR SUM(jl.credit_base) > 0
		ORDER BY coa.code
	`, entityIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EquityChangeRow
	for rows.Next() {
		var code, name, opening, closing string
		if err := rows.Scan(&code, &name, &opening, &closing); err != nil {
			return nil, err
		}
		od, _ := decimal.NewFromString(opening)
		cd, _ := decimal.NewFromString(closing)
		out = append(out, EquityChangeRow{
			AccountCode: code, AccountName: name,
			Opening: opening, Closing: closing, Movement: cd.Sub(od).String(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Retained earnings: net income for the period is the movement.
	ni, err := r.NetIncome(ctx, from, to, entityIDs)
	if err != nil {
		return nil, err
	}
	if !ni.IsZero() {
		out = append(out, EquityChangeRow{
			AccountCode: "NET-INCOME", AccountName: "Current Period Earnings",
			Opening: "0", Movement: ni.String(), Closing: ni.String(),
		})
	}
	return out, nil
}
