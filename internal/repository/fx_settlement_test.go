package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
)

// Phase 1: settling a foreign AR at a rate that moved since the invoice was
// booked recognises a realized FX gain to 7200, and the mixed-currency entry
// (USD legs + UGX gain line) still balances in base currency.
func TestRealizedFXGainOnARPayment(t *testing.T) {
	repo, pool := rawPool(t)
	repo.SetBaseCurrency("UGX")
	ctx := context.Background()
	if err := repo.SeedChartOfAccounts(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := ledger.New(repo)

	day := time.Now().UTC()
	// Invoice booked at 3700; the open item captures that document rate.
	if err := repo.UpsertRate(ctx, "USD", "3700", day); err != nil {
		t.Fatalf("rate: %v", err)
	}
	docRef := "INV-FX-" + uuid.NewString()
	item, err := repo.CreateAROpenItem(ctx, "CUST-FX", docRef, "usd sale", "10", "USD", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create AR: %v", err)
	}
	// The current (payment-date) rate has moved to 3800.
	if err := repo.UpsertRate(ctx, "USD", "3800", day); err != nil {
		t.Fatalf("rate2: %v", err)
	}

	pay, _, err := svc.ApplyARPayment(ctx, item.ID, decimal.RequireFromString("10"), "USD", "RCT-FX-1", "tester", nil)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	if pay.JournalEntryID == nil {
		t.Fatal("payment has no journal entry")
	}

	// Realized gain = 10 × (3800 − 3700) = 1,000 base, credited to 7200.
	var gain string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.credit_base), 0)::text
		FROM journal_lines jl JOIN chart_of_accounts coa ON coa.id = jl.account_id
		WHERE jl.journal_entry_id = $1 AND coa.code = '7200'
	`, *pay.JournalEntryID).Scan(&gain); err != nil {
		t.Fatalf("gain query: %v", err)
	}
	if decimal.RequireFromString(gain).Cmp(decimal.NewFromInt(1000)) != 0 {
		t.Fatalf("realized FX gain = %s; want 1000", gain)
	}

	// The mixed-currency entry must balance in BASE currency.
	var d, cr string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(debit_base),0)::text, COALESCE(SUM(credit_base),0)::text
		FROM journal_lines WHERE journal_entry_id = $1
	`, *pay.JournalEntryID).Scan(&d, &cr); err != nil {
		t.Fatalf("balance query: %v", err)
	}
	if decimal.RequireFromString(d).Cmp(decimal.RequireFromString(cr)) != 0 {
		t.Fatalf("entry not balanced in base: debit %s vs credit %s", d, cr)
	}
}
