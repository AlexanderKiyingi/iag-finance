package ledger

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Inventory movement postings.
//
// Production spent a release booking nothing: warehouse emitted
// production_consume and production_output, this mapping recognised neither,
// and the default treated them as cost-neutral. Nothing compared the two lists,
// so the services drifted apart quietly. TestEveryWarehouseMovementIsAccounted
// is the check that would have caught it.

// warehouseMovementTypes mirrors the constants in
// iag-warehouse/internal/models/models.go. It is duplicated deliberately: the
// two services are separate modules, so this is the seam where a new movement
// type has to be acknowledged rather than silently ignored.
var warehouseMovementTypes = []string{
	"receipt",
	"issue",
	"adjustment",
	"transfer",
	"pick",
	"return",
	"production_consume",
	"production_output",
	"asset_checkin",
	"asset_checkout",
	"asset_dispose",
}

func TestEveryWarehouseMovementIsAccounted(t *testing.T) {
	for _, mt := range warehouseMovementTypes {
		booked := postingFor(mt)
		_, neutral := CostNeutralMovements[mt]

		switch {
		case booked != nil && neutral:
			t.Errorf("%q is both booked and declared cost-neutral — decide which", mt)
		case booked == nil && !neutral:
			t.Errorf("%q reaches neither a posting nor the cost-neutral list, so it falls "+
				"into the default and books nothing. Map it, or declare it neutral with a reason.", mt)
		}
	}
}

// postingFor returns the lines a movement type would book, or nil when it falls
// through to the cost-neutral default.
func postingFor(movementType string) []LineInput {
	lines := linesForMovement(movementType, decimal.NewFromInt(100), decimal.NewFromInt(100), "memo")
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func TestProductionMovesValueThroughWIP(t *testing.T) {
	amt := decimal.NewFromInt(500)

	consume := linesForMovement("production_consume", amt, amt, "memo")
	assertPosting(t, "production_consume", consume, map[string]decimal.Decimal{
		wipAccount:       amt,       // material becomes work in progress
		inventoryAccount: amt.Neg(), // and leaves stock
	})

	output := linesForMovement("production_output", amt, amt, "memo")
	assertPosting(t, "production_output", output, map[string]decimal.Decimal{
		inventoryAccount: amt,       // finished goods arrive
		wipAccount:       amt.Neg(), // clearing the order's WIP
	})
}

// A consume and an output of equal value must leave WIP at zero — the
// invariant that says the two halves of material-only costing agree.
func TestConsumeAndOutputClearWIP(t *testing.T) {
	amt := decimal.NewFromInt(1234)
	net := decimal.Zero
	for _, mt := range []string{"production_consume", "production_output"} {
		for _, l := range linesForMovement(mt, amt, amt, "memo") {
			if l.AccountCode == wipAccount {
				net = net.Add(l.Debit).Sub(l.Credit)
			}
		}
	}
	if !net.IsZero() {
		t.Fatalf("WIP nets to %s across a consume and an equal output, want zero", net)
	}
}

func TestProductionDoesNotTouchCOGS(t *testing.T) {
	// Material-only costing keeps production out of cost of sales entirely:
	// value moves stock → WIP → stock, and only a later issue expenses it.
	for _, mt := range []string{"production_consume", "production_output"} {
		for _, l := range linesForMovement(mt, decimal.NewFromInt(10), decimal.NewFromInt(10), "memo") {
			if l.AccountCode == cogsAccount {
				t.Errorf("%s posts to COGS; production should move value through WIP, not expense it", mt)
			}
		}
	}
}

func TestInventoryPostingsBalance(t *testing.T) {
	amt := decimal.NewFromInt(777)
	for _, mt := range warehouseMovementTypes {
		lines := linesForMovement(mt, amt, amt, "memo")
		if len(lines) == 0 {
			continue
		}
		debit, credit := decimal.Zero, decimal.Zero
		for _, l := range lines {
			debit = debit.Add(l.Debit)
			credit = credit.Add(l.Credit)
		}
		if !debit.Equal(credit) {
			t.Errorf("%s: debits %s != credits %s", mt, debit, credit)
		}
	}
}

func assertPosting(t *testing.T, name string, lines []LineInput, want map[string]decimal.Decimal) {
	t.Helper()
	got := map[string]decimal.Decimal{}
	for _, l := range lines {
		got[l.AccountCode] = got[l.AccountCode].Add(l.Debit).Sub(l.Credit)
	}
	for account, wantNet := range want {
		if !got[account].Equal(wantNet) {
			t.Errorf("%s: account %s net %s, want %s", name, account, got[account], wantNet)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: touched %d accounts, want %d", name, len(got), len(want))
	}
}
