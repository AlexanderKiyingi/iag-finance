package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/db"
	"github.com/iag-finance/backend/internal/repository"
)

func testPool(t *testing.T) *repository.Repository {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return repository.New(pool)
}

func TestLinkAROpenItemByDocumentRef(t *testing.T) {
	t.Parallel()
	repo := testPool(t)
	ctx := context.Background()

	docRef := "TEST-AR-" + uuid.NewString()
	item, err := repo.CreateAROpenItem(ctx, "CUST-1", docRef, "test sale", "1000", "UGX", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create AR: %v", err)
	}
	if item.JournalEntryID != nil {
		t.Fatal("expected no journal link on create")
	}

	entry, err := repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber: "JE-TEST-AR-" + uuid.NewString()[:8],
		Description: "test",
		Status:      "posted",
		Lines: []repository.ResolvedLine{
			{AccountID: mustAccountID(t, repo, ctx, "1100"), Debit: decimal.NewFromInt(1000), LineOrder: 0},
			{AccountID: mustAccountID(t, repo, ctx, "4000"), Credit: decimal.NewFromInt(1000), LineOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}

	eventID := "sale.completed:" + docRef
	if err := repo.LinkAROpenItemByDocumentRef(ctx, docRef, entry.ID, eventID); err != nil {
		t.Fatalf("link: %v", err)
	}

	items, err := repo.ListAROpenItems(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var linked bool
	for _, it := range items {
		if it.ID == item.ID {
			linked = it.JournalEntryID != nil && *it.JournalEntryID == entry.ID
			if it.SourceEventID == nil || *it.SourceEventID != eventID {
				t.Fatalf("source_event_id = %v, want %q", it.SourceEventID, eventID)
			}
		}
	}
	if !linked {
		t.Fatal("AR item was not linked to journal entry")
	}
}

func TestLinkAPOpenItemByDocumentRef(t *testing.T) {
	t.Parallel()
	repo := testPool(t)
	ctx := context.Background()

	docRef := "TEST-AP-" + uuid.NewString()
	item, err := repo.CreateAPOpenItem(ctx, "VEND-1", docRef, "test invoice", "500", "UGX", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create AP: %v", err)
	}

	entry, err := repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber: "JE-TEST-AP-" + uuid.NewString()[:8],
		Description: "test",
		Status:      "posted",
		Lines: []repository.ResolvedLine{
			{AccountID: mustAccountID(t, repo, ctx, "5000"), Debit: decimal.NewFromInt(500), LineOrder: 0},
			{AccountID: mustAccountID(t, repo, ctx, "2000"), Credit: decimal.NewFromInt(500), LineOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}

	eventID := "invoice.posted:" + docRef
	if err := repo.LinkAPOpenItemByDocumentRef(ctx, docRef, entry.ID, eventID); err != nil {
		t.Fatalf("link: %v", err)
	}

	items, err := repo.ListAPOpenItems(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var linked bool
	for _, it := range items {
		if it.ID == item.ID {
			linked = it.JournalEntryID != nil && *it.JournalEntryID == entry.ID
		}
	}
	if !linked {
		t.Fatal("AP item was not linked to journal entry")
	}
}

func mustAccountID(t *testing.T, repo *repository.Repository, ctx context.Context, code string) uuid.UUID {
	t.Helper()
	if err := repo.SeedChartOfAccounts(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	acct, err := repo.GetAccountByCode(ctx, code)
	if err != nil || acct == nil {
		t.Fatalf("account %s: %v", code, err)
	}
	return acct.ID
}
