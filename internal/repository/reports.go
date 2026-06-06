package repository

import (
	"context"
	"time"
)

type ARAgingBucket struct {
	Label  string `json:"label"`
	Amount string `json:"amount"`
	Count  int    `json:"count"`
}

type PLRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Amount      string `json:"amount"`
}

type BalanceSheetSection struct {
	AccountType string `json:"accountType"`
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Balance     string `json:"balance"`
}

func (r *Repository) ARAging(ctx context.Context) ([]ARAgingBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN due_date IS NULL OR due_date >= CURRENT_DATE THEN 'current'
				WHEN due_date >= CURRENT_DATE - INTERVAL '30 days' THEN '1-30'
				WHEN due_date >= CURRENT_DATE - INTERVAL '60 days' THEN '31-60'
				WHEN due_date >= CURRENT_DATE - INTERVAL '90 days' THEN '61-90'
				ELSE '90+'
			END AS bucket,
			COALESCE(SUM(amount - amount_paid), 0)::text AS open_amount,
			COUNT(*)::int
		FROM ar_open_items
		WHERE status IN ('open', 'partial')
		GROUP BY 1
		ORDER BY 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	order := []string{"current", "1-30", "31-60", "61-90", "90+"}
	found := map[string]ARAgingBucket{}
	for rows.Next() {
		var label, amount string
		var count int
		if err := rows.Scan(&label, &amount, &count); err != nil {
			return nil, err
		}
		found[label] = ARAgingBucket{Label: label, Amount: amount, Count: count}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ARAgingBucket, 0, len(order))
	for _, label := range order {
		if b, ok := found[label]; ok {
			out = append(out, b)
		} else {
			out = append(out, ARAgingBucket{Label: label, Amount: "0", Count: 0})
		}
	}
	return out, nil
}

func (r *Repository) ProfitAndLoss(ctx context.Context) ([]PLRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name,
			CASE coa.account_type
				WHEN 'revenue' THEN (COALESCE(SUM(jl.credit), 0) - COALESCE(SUM(jl.debit), 0))::text
				WHEN 'expense' THEN (COALESCE(SUM(jl.debit), 0) - COALESCE(SUM(jl.credit), 0))::text
			END
		FROM chart_of_accounts coa
		LEFT JOIN journal_lines jl ON jl.account_id = coa.id
		LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
		WHERE coa.active = TRUE AND coa.account_type IN ('revenue', 'expense')
		GROUP BY coa.id, coa.code, coa.name, coa.account_type
		HAVING COALESCE(SUM(jl.debit), 0) > 0 OR COALESCE(SUM(jl.credit), 0) > 0
		ORDER BY coa.account_type, coa.code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PLRow
	for rows.Next() {
		var row PLRow
		if err := rows.Scan(&row.AccountCode, &row.AccountName, &row.Amount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) BalanceSheet(ctx context.Context) ([]BalanceSheetSection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.account_type, coa.code, coa.name,
			CASE coa.account_type
				WHEN 'asset' THEN (COALESCE(SUM(jl.debit), 0) - COALESCE(SUM(jl.credit), 0))::text
				WHEN 'liability' THEN (COALESCE(SUM(jl.credit), 0) - COALESCE(SUM(jl.debit), 0))::text
				WHEN 'equity' THEN (COALESCE(SUM(jl.credit), 0) - COALESCE(SUM(jl.debit), 0))::text
			END
		FROM chart_of_accounts coa
		LEFT JOIN journal_lines jl ON jl.account_id = coa.id
		LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
		WHERE coa.active = TRUE AND coa.account_type IN ('asset', 'liability', 'equity')
		GROUP BY coa.id, coa.account_type, coa.code, coa.name
		HAVING COALESCE(SUM(jl.debit), 0) > 0 OR COALESCE(SUM(jl.credit), 0) > 0
		ORDER BY coa.account_type, coa.code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalanceSheetSection
	for rows.Next() {
		var row BalanceSheetSection
		if err := rows.Scan(&row.AccountType, &row.AccountCode, &row.AccountName, &row.Balance); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// FinanceSummary aggregates AR metrics for BFF consumers (e.g. DMS).
type FinanceSummary struct {
	ARBalance    string    `json:"arBalance"`
	Overdue      string    `json:"overdue"`
	Collected    string    `json:"collected"`
	OpenItems    int       `json:"openItems"`
	GeneratedAt  time.Time `json:"generatedAt"`
}

func (r *Repository) FinanceSummary(ctx context.Context) (FinanceSummary, error) {
	var s FinanceSummary
	s.GeneratedAt = time.Now().UTC()
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount - amount_paid), 0)::text,
			COALESCE(SUM(CASE WHEN due_date < CURRENT_DATE THEN amount - amount_paid ELSE 0 END), 0)::text,
			COALESCE(SUM(amount_paid), 0)::text,
			COUNT(*) FILTER (WHERE status IN ('open', 'partial'))::int
		FROM ar_open_items
	`).Scan(&s.ARBalance, &s.Overdue, &s.Collected, &s.OpenItems)
	return s, err
}
