package consumer

import (
	"testing"

	"github.com/iag-finance/backend/internal/ledger"
)

// A coffee payout used to capitalise straight to Inventory because a cherry
// purchase had no prior finance document to settle against. It has one now, so
// the payout is a settlement like any other — but the two halves deploy
// separately, and the flag is what keeps the in-between states honest.

func TestCoffeePayoutCapitalisesUntilThePayableExists(t *testing.T) {
	// Flag off: iag-supply-chain is not yet emitting the payable, so this
	// settlement is still the only thing that puts the coffee into stock.
	code, _ := disbursementDebit("coffee_payout", false)

	if code != "1400" {
		t.Fatalf("debit = %s, want 1400 Inventory — with no payable raised, routing to clearing would leave the coffee in no asset account at all", code)
	}
}

func TestCoffeePayoutSettlesThePayableOnceItExists(t *testing.T) {
	// Flag on: the purchase already capitalised the coffee and raised the
	// payable, so capitalising again here would count the same crop twice.
	code, _ := disbursementDebit("coffee_payout", true)

	if code != ledger.PaymentsClearingAccount {
		t.Fatalf("debit = %s, want %s — the coffee is already in stock from the purchase", code, ledger.PaymentsClearingAccount)
	}
}

func TestEveryOtherDisbursementIsUnaffectedByTheFlag(t *testing.T) {
	for _, category := range []string{"vendor_settlement", "payroll", "refund", ""} {
		off, _ := disbursementDebit(category, false)
		on, _ := disbursementDebit(category, true)

		if off != ledger.PaymentsClearingAccount || on != ledger.PaymentsClearingAccount {
			t.Errorf("category %q: debits %s/%s, want %s either way — only coffee_payout is gated",
				category, off, on, ledger.PaymentsClearingAccount)
		}
	}
}

func TestTheCoffeePayableReferenceIsDerivedFromSCM(t *testing.T) {
	// Stable and derived only from SCM's business id: a redelivery must collide
	// with the payable already booked rather than raise a second one, and the
	// settlement has to be able to find it later.
	if got := coffeePayableRef("PAY-2026-001"); got != "CHERRY-PAY-2026-001" {
		t.Fatalf("documentRef = %q, want CHERRY-PAY-2026-001", got)
	}
}
