package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/chainaudit"
	"github.com/iag-finance/backend/internal/db"
	"github.com/iag-finance/backend/internal/repository"
)

// rawPool connects and returns the underlying pool alongside the repo so tests
// can assert on raw rows (e.g. tamper the audit chain). Skips without a DB.
func rawPool(t *testing.T) (*repository.Repository, *pgxpool.Pool) {
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
	return repository.New(pool), pool
}

func balancedLines(t *testing.T, repo *repository.Repository, ctx context.Context, amount int64) []repository.ResolvedLine {
	return []repository.ResolvedLine{
		{AccountID: mustAccountID(t, repo, ctx, "1100"), Debit: decimal.NewFromInt(amount), LineOrder: 0},
		{AccountID: mustAccountID(t, repo, ctx, "4000"), Credit: decimal.NewFromInt(amount), LineOrder: 1},
	}
}

// 1B: the database refuses to commit an unbalanced *posted* entry.
func TestBalanceTriggerRejectsUnbalancedPosted(t *testing.T) {
	repo, _ := rawPool(t)
	ctx := context.Background()

	_, err := repo.CreateJournalEntry(ctx, repository.CreateJournalParams{
		EntryNumber: "JE-UNBAL-" + uuid.NewString()[:8],
		Description: "unbalanced",
		Status:      "posted",
		Lines: []repository.ResolvedLine{
			{AccountID: mustAccountID(t, repo, ctx, "1100"), Debit: decimal.NewFromInt(100), LineOrder: 0},
			{AccountID: mustAccountID(t, repo, ctx, "4000"), Credit: decimal.NewFromInt(60), LineOrder: 1},
		},
	})
	if err == nil {
		t.Fatal("expected the balance constraint trigger to reject an unbalanced posted entry")
	}
}

// 1C: booking the same source event twice yields exactly one entry.
func TestBookPostedEntryIdempotent(t *testing.T) {
	repo, pool := rawPool(t)
	ctx := context.Background()

	eventID := "test.book:" + uuid.NewString()
	first, err := repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "idempotency probe",
		Lines:       balancedLines(t, repo, ctx, 250),
	}, eventID, "test.event", time.Now().UTC(), nil, nil)
	if err != nil {
		t.Fatalf("first book: %v", err)
	}
	second, err := repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "idempotency probe (replay)",
		Lines:       balancedLines(t, repo, ctx, 250),
	}, eventID, "test.event", time.Now().UTC(), nil, nil)
	if err != nil {
		t.Fatalf("replay book: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay produced a new entry: %s != %s", first.ID, second.ID)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM journal_entries WHERE source_event_id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one journal entry for the event, got %d", count)
	}
}

// 1E: a credit note larger than the open balance is rejected.
func TestAdjustmentFloorRejectsOverCredit(t *testing.T) {
	repo, _ := rawPool(t)
	ctx := context.Background()

	docRef := "TEST-FLOOR-" + uuid.NewString()
	if _, err := repo.CreateAROpenItem(ctx, "CUST-FLOOR", docRef, "sale", "100", "UGX", nil, nil, nil, nil); err != nil {
		t.Fatalf("create AR: %v", err)
	}

	lines := []repository.ResolvedLine{
		{AccountID: mustAccountID(t, repo, ctx, "4000"), Debit: decimal.NewFromInt(500), LineOrder: 0},
		{AccountID: mustAccountID(t, repo, ctx, "1100"), Credit: decimal.NewFromInt(500), LineOrder: 1},
	}
	_, err := repo.BookAdjustment(ctx, repository.BookAdjustmentParams{
		EventID: "adjustment.ar:credit_note:CN-" + docRef, EventType: "finance.adjustment",
		Description: "over-credit", Source: "iag.finance", Direction: "ar",
		Delta: decimal.NewFromInt(-500), Lines: lines,
		Adjustment: repository.CreateAdjustmentParams{
			Kind: "credit_note", Direction: "ar", OriginalDocumentRef: docRef,
			DocumentRef: "CN-" + docRef, PartyRef: "CUST-FLOOR",
			Amount: decimal.NewFromInt(500), Currency: "UGX",
		},
	}, time.Now().UTC(), nil)
	if err != repository.ErrAdjustmentTooLarge {
		t.Fatalf("expected ErrAdjustmentTooLarge, got %v", err)
	}
}

// 1G: the chain verifies, and tampering any field is detected.
func TestAuditChainVerifyDetectsTamper(t *testing.T) {
	repo, pool := rawPool(t)
	ctx := context.Background()
	store := &chainaudit.Store{Pg: pool}

	// Append a chained entry via a real booking.
	if _, err := repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "chain probe",
		Lines:       balancedLines(t, repo, ctx, 75),
	}, "test.chain:"+uuid.NewString(), "test.event", time.Now().UTC(), nil, &repository.AuditInfo{
		Actor: "tester", EventType: "ledger.booked", Message: "chain probe",
	}); err != nil {
		t.Fatalf("book with audit: %v", err)
	}

	res, err := store.Verify(ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected a valid chain, got broken at %v: %s", res.BrokenAt, res.Reason)
	}

	// Tamper the latest row's message; verification must now fail.
	if _, err := pool.Exec(ctx, `
		UPDATE audit_events SET message = message || '(tampered)'
		WHERE id = (SELECT MAX(id) FROM audit_events)
	`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	res, err = store.Verify(ctx)
	if err != nil {
		t.Fatalf("verify after tamper: %v", err)
	}
	if res.Valid {
		t.Fatal("expected verification to detect the tampered row")
	}
}
