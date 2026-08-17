package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The invoicer runs on every replica and every replica sees the same due
// schedules, so the claim is the only thing standing between one invoice and
// several. These tests pin that behaviour against a real database, because the
// guarantee being relied on is Postgres's — a conditional UPDATE reports one
// row affected to exactly one caller.
func TestClaimRecurringIsExclusive(t *testing.T) {
	repo, pool := rawPool(t)
	ctx := context.Background()

	id := uuid.New()
	due := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	next := due.AddDate(0, 1, 0)

	if _, err := pool.Exec(ctx, `
		INSERT INTO recurring_invoices (id, customer_ref, currency, cadence, next_run, template, notes, active)
		VALUES ($1, 'CUST-CLAIM', 'UGX', 'monthly', $2, '[]'::jsonb, '', true)
	`, id, due); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM recurring_invoices WHERE id = $1`, id)
	})

	first, err := repo.ClaimRecurring(ctx, id, due, next)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !first {
		t.Fatal("the first caller must win the claim")
	}

	// The second replica read the same row before either advanced it, so it
	// arrives with the same expectation and must be turned away.
	second, err := repo.ClaimRecurring(ctx, id, due, next)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if second {
		t.Fatal("a second claim on an already-advanced schedule generated a duplicate invoice")
	}

	var stored time.Time
	if err := pool.QueryRow(ctx, `SELECT next_run FROM recurring_invoices WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !stored.UTC().Truncate(24 * time.Hour).Equal(next) {
		t.Fatalf("next_run = %s, want %s", stored, next)
	}
}

// An inactive or already-advanced schedule must not be claimable by a stale
// read, which is the same protection viewed from the other side.
func TestClaimRecurringRejectsStaleExpectation(t *testing.T) {
	repo, pool := rawPool(t)
	ctx := context.Background()

	id := uuid.New()
	due := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)

	if _, err := pool.Exec(ctx, `
		INSERT INTO recurring_invoices (id, customer_ref, currency, cadence, next_run, template, notes, active)
		VALUES ($1, 'CUST-STALE', 'UGX', 'monthly', $2, '[]'::jsonb, '', true)
	`, id, due); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM recurring_invoices WHERE id = $1`, id)
	})

	stale := due.AddDate(0, 0, -30)
	claimed, err := repo.ClaimRecurring(ctx, id, stale, due.AddDate(0, 1, 0))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed {
		t.Fatal("a claim carrying a stale next_run must not succeed")
	}
}
