package ledger

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// EventRef identifies the source event a posting derives from, and when the
// fact it reports actually happened.
//
// OccurredAt is the piece that used to be missing. Every event-driven posting
// dated itself by arrival — time.Now() at the instant the consumer read the
// message — so an event delivered late, replayed from the DLQ, or held up
// behind a lagging partition landed in whatever month finance happened to read
// it. A goods receipt dated 30 June booked into July with nothing on the entry
// to say it had moved, and its foreign-currency amounts converted at the wrong
// day's rate. Both errors are silent: the entry balances either way.
type EventRef struct {
	ID            string
	Type          string
	Source        string
	CorrelationID string
	// OccurredAt is the envelope time the producer stamped. Zero means it sent
	// none — an emitter predating the contract, or an unparseable value — and
	// the posting falls back to arrival time.
	OccurredAt time.Time
}

// futureDateTolerance bounds how far ahead an event may claim to have happened
// before its date is treated as a producer clock fault rather than a genuinely
// forward-dated transaction. A day absorbs timezone skew between services
// without admitting a wrong year.
const futureDateTolerance = 24 * time.Hour

// postingWindow is the pair of dates an event-driven entry carries.
type postingWindow struct {
	// Transaction is the date the business fact occurred. It is the measurement
	// date for FX — IAS 21 records a transaction at the spot rate ruling on the
	// date it happens — whatever period the entry ultimately files under.
	Transaction time.Time
	// Accounting is the date the entry is filed under, and what fiscal-period
	// control keys off. Normally the same day as Transaction.
	Accounting time.Time
	// Deferred is true when the transaction's own period was closed and the
	// entry was filed into the current open period instead.
	Deferred bool
}

// transactionDate reads the date a posting should be measured at, and reports
// whether the event's own claim had to be discarded.
//
// Pure so the fallback rules are testable without a database — the same reason
// linesForMovement is separated from BookInventoryMovement.
func transactionDate(occurredAt, now time.Time) (txn time.Time, clamped bool) {
	if occurredAt.IsZero() {
		return now, false
	}
	txn = occurredAt.UTC()
	if txn.After(now.Add(futureDateTolerance)) {
		// Booking this would put revenue or expense in a period that has not
		// happened yet, where no month-end close will ever catch it.
		return now, true
	}
	return txn, false
}

// chooseWindow decides where a posting files, given whether the transaction's
// own period and the current period are closed.
//
// When the transaction's period is closed the entry files into the current open
// period rather than being refused: refusing would leave a real transaction
// unbooked whenever a redelivery arrives after a month-end close, and a
// dead-letter queue is not a ledger. Both periods closed is a genuine stop —
// there is no open month to file into, and inventing one is worse than making
// an operator look.
//
// Pure, for the same reason as transactionDate.
func chooseWindow(txn, now time.Time, txnPeriodClosed, nowPeriodClosed bool) (postingWindow, error) {
	if !txnPeriodClosed {
		return postingWindow{Transaction: txn, Accounting: txn}, nil
	}
	if nowPeriodClosed {
		return postingWindow{}, ErrPeriodClosed
	}
	return postingWindow{Transaction: txn, Accounting: now, Deferred: true}, nil
}

// resolvePostingWindow decides when an event-driven entry is dated, reading
// fiscal-period state from the database and applying the rules above.
func (s *Service) resolvePostingWindow(ctx context.Context, ref EventRef) (postingWindow, error) {
	now := time.Now().UTC()
	txn, clamped := transactionDate(ref.OccurredAt, now)
	if clamped {
		slog.Warn("event occurredAt is implausibly far ahead; dating the posting by arrival",
			"eventId", ref.ID, "eventType", ref.Type, "occurredAt", ref.OccurredAt.UTC().Format(time.RFC3339))
	}

	txnPeriod, nowPeriod := txn.Format("2006-01"), now.Format("2006-01")
	txnClosed, err := s.repo.IsPeriodClosed(ctx, txnPeriod)
	if err != nil {
		return postingWindow{}, err
	}
	// Same month is the ordinary case; don't ask twice for one answer.
	nowClosed := txnClosed
	if txnClosed && txnPeriod != nowPeriod {
		if nowClosed, err = s.repo.IsPeriodClosed(ctx, nowPeriod); err != nil {
			return postingWindow{}, err
		}
	}

	window, err := chooseWindow(txn, now, txnClosed, nowClosed)
	if err != nil {
		return postingWindow{}, err
	}
	if window.Deferred {
		slog.Warn("event dated into a closed period; filing it in the current open period",
			"eventId", ref.ID, "eventType", ref.Type,
			"transactionDate", window.Transaction.Format("2006-01-02"),
			"accountingDate", window.Accounting.Format("2006-01-02"))
	}
	return window, nil
}

// describeDeferral appends the original transaction date to an entry
// description when the posting was filed into a later period, so the entry
// carries the reason it sits where it does.
func describeDeferral(description string, w postingWindow) string {
	if !w.Deferred {
		return description
	}
	return fmt.Sprintf("%s [dated %s; period closed, filed %s]",
		description, w.Transaction.Format("2006-01-02"), w.Accounting.Format("2006-01-02"))
}
