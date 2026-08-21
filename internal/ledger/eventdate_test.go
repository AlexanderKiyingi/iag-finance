package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Event-driven postings used to be dated by arrival: time.Now() at the instant
// the consumer read the message. A goods receipt dated 30 June that arrived on
// 2 July — a redelivery, a lagging partition, a DLQ replay — booked into July,
// and converted its foreign currency at the wrong day's rate. Both are silent
// errors, because the entry balances either way.

var now = time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)

func TestTransactionDateUsesTheEventsOwnDate(t *testing.T) {
	occurred := time.Date(2026, 6, 30, 16, 45, 0, 0, time.UTC)

	txn, clamped := transactionDate(occurred, now)

	if clamped {
		t.Error("a past date is not a clock fault and must not be clamped")
	}
	if !txn.Equal(occurred) {
		t.Errorf("transaction date = %s, want the event's own %s", txn, occurred)
	}
}

func TestTransactionDateFallsBackToArrivalWhenTheEventCarriesNone(t *testing.T) {
	// Emitters predating the envelope-time contract, and unparseable times,
	// both arrive here as the zero time. Arrival is the only date available.
	txn, clamped := transactionDate(time.Time{}, now)

	if clamped {
		t.Error("an absent date is not a clock fault")
	}
	if !txn.Equal(now) {
		t.Errorf("transaction date = %s, want arrival %s", txn, now)
	}
}

func TestTransactionDateClampsAnImplausibleFutureDate(t *testing.T) {
	// A producer with a broken clock would otherwise put revenue in a period
	// that has not happened, where no month-end close will ever catch it.
	txn, clamped := transactionDate(now.AddDate(1, 0, 0), now)

	if !clamped {
		t.Error("a date a year ahead should be treated as a clock fault")
	}
	if !txn.Equal(now) {
		t.Errorf("transaction date = %s, want arrival %s", txn, now)
	}
}

func TestTransactionDateToleratesTimezoneSkew(t *testing.T) {
	// Services disagreeing by a few hours is ordinary; only a wild date is a fault.
	skewed := now.Add(3 * time.Hour)

	txn, clamped := transactionDate(skewed, now)

	if clamped {
		t.Error("a few hours ahead is timezone skew, not a clock fault")
	}
	if !txn.Equal(skewed) {
		t.Errorf("transaction date = %s, want %s", txn, skewed)
	}
}

func TestOpenPeriodFilesTheEntryOnItsTransactionDate(t *testing.T) {
	txn := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	w, err := chooseWindow(txn, now, false, false)
	if err != nil {
		t.Fatalf("chooseWindow: %v", err)
	}

	if !w.Accounting.Equal(txn) {
		t.Errorf("accounting date = %s, want the transaction date %s", w.Accounting, txn)
	}
	if w.Deferred {
		t.Error("nothing was deferred: the transaction's own period is open")
	}
}

func TestClosedPeriodFilesTheEntryForwardRatherThanLosingIt(t *testing.T) {
	// Refusing would strand a real transaction whenever a redelivery arrives
	// after a month-end close. A dead-letter queue is not a ledger.
	txn := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	w, err := chooseWindow(txn, now, true, false)
	if err != nil {
		t.Fatalf("chooseWindow: %v", err)
	}

	if !w.Deferred {
		t.Fatal("filing into a later period must be flagged, not silent")
	}
	if !w.Accounting.Equal(now) {
		t.Errorf("accounting date = %s, want the current open period %s", w.Accounting, now)
	}
	// The measurement date does not move with the filing date: IAS 21 records a
	// transaction at the rate ruling when it happened.
	if !w.Transaction.Equal(txn) {
		t.Errorf("transaction date = %s, want it unchanged at %s", w.Transaction, txn)
	}
}

func TestNoOpenPeriodIsRefused(t *testing.T) {
	txn := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	_, err := chooseWindow(txn, now, true, true)

	if !errors.Is(err, ErrPeriodClosed) {
		t.Fatalf("err = %v, want ErrPeriodClosed — there is nowhere honest to file it", err)
	}
}

func TestDeferralIsRecordedOnTheEntryItself(t *testing.T) {
	// A log line is not where anyone looks at year-end; the general ledger is.
	w := postingWindow{
		Transaction: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Accounting:  now,
		Deferred:    true,
	}

	got := describeDeferral("Goods received accrual — PO 42", w)

	if !strings.Contains(got, "2026-06-30") {
		t.Errorf("description %q does not say when the transaction actually happened", got)
	}
	if !strings.Contains(got, "2026-08-18") {
		t.Errorf("description %q does not say where it was filed", got)
	}
}

func TestUndeferredDescriptionIsLeftAlone(t *testing.T) {
	w := postingWindow{Transaction: now, Accounting: now}

	if got := describeDeferral("Sale completed — INV-1", w); got != "Sale completed — INV-1" {
		t.Errorf("description = %q, want it unchanged when nothing was deferred", got)
	}
}
