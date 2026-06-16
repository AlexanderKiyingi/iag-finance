package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// 2E: a foreign-currency entry stores base-currency amounts converted at the
// rate, so a trial balance (which sums base) reflects the converted values.
func TestFXBaseConversion(t *testing.T) {
	repo, pool := rawPool(t)
	repo.SetBaseCurrency("UGX")
	ctx := context.Background()

	day := time.Now().UTC()
	if err := repo.UpsertRate(ctx, "USD", "3700", day); err != nil {
		t.Fatalf("upsert rate: %v", err)
	}
	rate, err := repo.GetRate(ctx, "USD", day)
	if err != nil || !rate.Equal(decimal.RequireFromString("3700")) {
		t.Fatalf("GetRate USD = %s, %v; want 3700", rate, err)
	}
	// Base currency is always 1.
	if one, _ := repo.GetRate(ctx, "UGX", day); !one.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("base-currency rate = %s; want 1", one)
	}

	eventID := "fx.probe:" + uuid.NewString()
	entry, err := repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "USD sale",
		Currency:    "USD",
		FXRate:      rate,
		Lines: []repository.ResolvedLine{
			{AccountID: mustAccountID(t, repo, ctx, "1100"), Debit: decimal.NewFromInt(10), LineOrder: 0},
			{AccountID: mustAccountID(t, repo, ctx, "4000"), Credit: decimal.NewFromInt(10), LineOrder: 1},
		},
	}, eventID, "test.event", day, nil, nil)
	if err != nil {
		t.Fatalf("book USD entry: %v", err)
	}

	// 10 USD × 3700 = 37,000 base on each side.
	var debitBase, creditBase, currency string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(debit_base),0)::text, COALESCE(SUM(credit_base),0)::text, MAX(currency)
		FROM journal_lines WHERE journal_entry_id = $1
	`, entry.ID).Scan(&debitBase, &creditBase, &currency); err != nil {
		t.Fatalf("scan base amounts: %v", err)
	}
	if decimal.RequireFromString(debitBase).Cmp(decimal.NewFromInt(37000)) != 0 {
		t.Fatalf("debit_base = %s; want 37000", debitBase)
	}
	if decimal.RequireFromString(creditBase).Cmp(decimal.NewFromInt(37000)) != 0 {
		t.Fatalf("credit_base = %s; want 37000", creditBase)
	}
	if currency != "USD" {
		t.Fatalf("line currency = %s; want USD", currency)
	}
}
