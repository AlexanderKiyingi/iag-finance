package db

import (
	"regexp"
	"strings"
	"testing"
)

// Finance runs every Kafka consumer with NoopDedupe — deliberately. It keeps no
// record of which events it has seen and instead relies on each handler being
// idempotent on a business key, so a redelivery lands on the same row rather
// than a second one. That is the stronger design: it survives a lost dedupe
// table, and it also covers a caller who retries over HTTP rather than Kafka.
//
// It rests entirely on two database invariants. Every GL posting from an event
// goes through BookPostedEntry, which leans on the unique source_event_id; every
// payable goes through CreateAPItem, which leans on the unique document_ref and
// reads the resulting violation as "already booked". Drop either index and
// finance silently starts double-booking on the next redelivery, with nothing in
// the code to catch it. Hence this test.

func migrationsText(t *testing.T) string {
	t.Helper()
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("reading migrations: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		raw, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

// A source event books at most one journal entry. This is the backstop behind
// every Book* path in the ledger and the reason a redelivered event returns the
// entry it already booked instead of posting a second one.
func TestASourceEventCanBookOnlyOneJournalEntry(t *testing.T) {
	sql := migrationsText(t)
	create := regexp.MustCompile(`(?is)CREATE\s+UNIQUE\s+INDEX[^;]*ON\s+journal_entries\s*\(\s*source_event_id\s*\)`)
	if !create.MatchString(sql) {
		t.Fatal("no unique index on journal_entries(source_event_id): every event-sourced posting could double-book")
	}
	assertNotDropped(t, sql, "uq_journal_source_event")
}

// A document reference identifies one payable. CreateAPItem treats the unique
// violation as success, which is what makes a redelivered invoice, contract
// payment or fuel record land on the payable that is already there.
func TestADocumentReferenceIdentifiesOnePayable(t *testing.T) {
	sql := migrationsText(t)
	create := regexp.MustCompile(`(?is)CREATE\s+UNIQUE\s+INDEX[^;]*ON\s+ap_open_items\s*\(\s*document_ref\s*\)`)
	if !create.MatchString(sql) {
		t.Fatal("no unique index on ap_open_items(document_ref): a redelivered invoice would raise a second payable")
	}
	assertNotDropped(t, sql, "idx_ap_document_ref")
}

// The receivable side carries the same guarantee, and settlement matches on it.
func TestADocumentReferenceIdentifiesOneReceivable(t *testing.T) {
	sql := migrationsText(t)
	create := regexp.MustCompile(`(?is)CREATE\s+UNIQUE\s+INDEX[^;]*ON\s+ar_open_items\s*\(\s*document_ref\s*\)`)
	if !create.MatchString(sql) {
		t.Fatal("no unique index on ar_open_items(document_ref): a redelivered sale would raise a second receivable")
	}
	assertNotDropped(t, sql, "idx_ar_document_ref")
}

// One settlement leaves one row in the Payments Clearing subledger. Without
// this, a redelivered payments.settled would add a second row and the 1050
// control reconciliation — the thing that exists to reveal drift — would itself
// report drift that was never there.
func TestASettlementIdentifiesOneClearingItem(t *testing.T) {
	sql := migrationsText(t)
	create := regexp.MustCompile(`(?is)CREATE\s+UNIQUE\s+INDEX[^;]*ON\s+payments_clearing_items\s*\(\s*instruction_id\s*\)`)
	if !create.MatchString(sql) {
		t.Fatal("no unique index on payments_clearing_items(instruction_id): a redelivered settlement would double-count the clearing account")
	}
	assertNotDropped(t, sql, "uq_payments_clearing_instruction")
}

// A later migration dropping the index is the failure this is really watching
// for: the CREATE stays in history and reads as though the guarantee holds.
func assertNotDropped(t *testing.T, sql, index string) {
	t.Helper()
	drop := regexp.MustCompile(`(?is)DROP\s+INDEX\s+(IF\s+EXISTS\s+)?` + regexp.QuoteMeta(index) + `\b`)
	if drop.MatchString(sql) {
		t.Errorf("%s is dropped by a later migration; finance would double-book on redelivery", index)
	}
}
