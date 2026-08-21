package ledger

import (
	"testing"

	"github.com/shopspring/decimal"
)

// One delivery against a purchase order raised two events that both credited
// GR/IR clearing — procurement.grn.posted, and warehouse.movement.posted for the
// same goods once they were put away. The vendor invoice cleared only one of
// them, so the cost was recognised twice (once in expense, once in inventory)
// and 2150 kept a permanent credit equal to the receipt value.
//
// Nothing would have flagged it: both entries balance.

func TestAGRNReceiptBooksOnlyFromProcurement(t *testing.T) {
	amt := decimal.NewFromInt(400)

	fromGRN := linesForMovement("receipt", SourceDocProcurementGRN, amt, amt, "memo")

	if len(fromGRN) != 0 {
		t.Fatalf("a receipt already accrued from procurement.grn.posted booked again: %v", fromGRN)
	}
}

func TestANonGRNReceiptStillBooks(t *testing.T) {
	// Stock arriving by any other route — field intake, a customer return, a
	// transfer in from another site — has no procurement accrual behind it, so
	// the warehouse movement is the only event that will ever book it.
	amt := decimal.NewFromInt(400)

	lines := linesForMovement("receipt", "", amt, amt, "memo")

	assertPosting(t, "receipt", lines, map[string]decimal.Decimal{
		inventoryAccount: amt,
		grirAccount:      amt.Neg(),
	})
}

// The two legs of one delivery must not both credit GR/IR. Summing them is the
// direct statement of the bug: it used to come to twice the receipt value.
func TestOneDeliveryCreditsGRIROnce(t *testing.T) {
	value := decimal.NewFromInt(400)

	grir := decimal.Zero
	for _, l := range grnAccrualLines(value, value) { // procurement's accrual
		if l.AccountCode == grIRClearingAccount {
			grir = grir.Add(l.Credit).Sub(l.Debit)
		}
	}
	for _, l := range linesForMovement("receipt", SourceDocProcurementGRN, value, value, "memo") { // warehouse's leg
		if l.AccountCode == grirAccount {
			grir = grir.Add(l.Credit).Sub(l.Debit)
		}
	}

	if !grir.Equal(value) {
		t.Fatalf("GR/IR credited %s for a %s delivery; the invoice clears it once, so anything else strands a balance", grir, value)
	}
}

func TestStockableReceiptsCapitaliseAndTheRestExpense(t *testing.T) {
	// A mixed receipt: goods for stock plus a non-stock line (a service or a
	// direct-to-site consumable) on the same purchase order.
	lines := grnAccrualLines(decimal.NewFromInt(1000), decimal.NewFromInt(700))

	assertPosting(t, "grn accrual", lines, map[string]decimal.Decimal{
		inventoryAccount:    decimal.NewFromInt(700),
		grniExpenseAccount:  decimal.NewFromInt(300),
		grIRClearingAccount: decimal.NewFromInt(1000).Neg(),
	})
}

func TestAReceiptWithNoSplitExpensesEverything(t *testing.T) {
	// What every emitter sent before the split existed, and what a replayed
	// event still sends. It must keep booking exactly as it did.
	lines := grnAccrualLines(decimal.NewFromInt(250), decimal.Zero)

	assertPosting(t, "grn accrual", lines, map[string]decimal.Decimal{
		grniExpenseAccount:  decimal.NewFromInt(250),
		grIRClearingAccount: decimal.NewFromInt(250).Neg(),
	})
}

// An upstream emitter that miscomputes the split must not be able to unbalance
// the ledger or conjure a credit out of a debit.
func TestANonsensicalSplitIsClamped(t *testing.T) {
	value := decimal.NewFromInt(500)

	for _, bad := range []decimal.Decimal{decimal.NewFromInt(-100), decimal.NewFromInt(900)} {
		lines := grnAccrualLines(value, bad)

		debit, credit := decimal.Zero, decimal.Zero
		for _, l := range lines {
			debit = debit.Add(l.Debit)
			credit = credit.Add(l.Credit)
		}
		if !debit.Equal(credit) {
			t.Errorf("inventoryValue=%s unbalanced the entry: debits %s != credits %s", bad, debit, credit)
		}
		if !credit.Equal(value) {
			t.Errorf("inventoryValue=%s changed the GR/IR credit to %s, want the received value %s", bad, credit, value)
		}
	}
}
