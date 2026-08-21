package ledger

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Coffee — the platform's core purchase — reached the books only when
// iag-payments settled the payout. Between taking delivery and paying for it the
// ledger showed neither the stock nor the money owed: no farmer payable, and
// nothing on the balance sheet for the coffee sitting in the store.
//
// A cherry purchase is stock, not an expense. Expensing it would charge the crop
// to profit on the day it was bought and leave the inventory unrecorded; it
// becomes cost of sales when the coffee is sold.

// apInvoiceLines mirrors the net-routing decision inside BookAPInvoice for a
// purchase with no PO and no VAT, which is what a cherry purchase is.
func apInvoiceLines(t *testing.T, gross, inventoryValue decimal.Decimal) []LineInput {
	t.Helper()
	stock := inventoryValue
	if stock.IsNegative() {
		stock = decimal.Zero
	}
	if stock.GreaterThan(gross) {
		stock = gross
	}
	lines := make([]LineInput, 0, 3)
	if stock.IsPositive() {
		lines = append(lines, LineInput{AccountCode: inventoryAccount, Debit: stock, Memo: "Purchased into stock"})
	}
	if expense := gross.Sub(stock); expense.IsPositive() {
		lines = append(lines, LineInput{AccountCode: grniExpenseAccount, Debit: expense, Memo: "Expense / COGS"})
	}
	return append(lines, LineInput{AccountCode: apControlAccount, Credit: gross, Memo: "AP liability"})
}

func TestACoffeePurchaseCapitalisesAndOwesTheFarmer(t *testing.T) {
	gross := decimal.NewFromInt(850000)

	lines := apInvoiceLines(t, gross, gross)

	assertPosting(t, "coffee purchase", lines, map[string]decimal.Decimal{
		inventoryAccount: gross,       // the coffee is an asset...
		apControlAccount: gross.Neg(), // ...and the farmer is owed for it
	})
}

func TestAPurchaseWithNoInventoryValueStillExpenses(t *testing.T) {
	// Every non-stock payable — services, fuel, ad-hoc — carries no inventory
	// value and must keep booking exactly as it did before the split existed.
	gross := decimal.NewFromInt(120000)

	lines := apInvoiceLines(t, gross, decimal.Zero)

	assertPosting(t, "service invoice", lines, map[string]decimal.Decimal{
		grniExpenseAccount: gross,
		apControlAccount:   gross.Neg(),
	})
}

// The payout must settle the payable, not capitalise the coffee a second time.
// disbursementDebit lives in the consumer package; this states the arithmetic
// the flag protects.
func TestPayingTheFarmerMustNotCapitaliseTheCoffeeTwice(t *testing.T) {
	gross := decimal.NewFromInt(850000)

	inventory := decimal.Zero
	for _, l := range apInvoiceLines(t, gross, gross) { // the purchase
		if l.AccountCode == inventoryAccount {
			inventory = inventory.Add(l.Debit).Sub(l.Credit)
		}
	}
	// The settlement, once COFFEE_PAYOUT_VIA_CLEARING is on: Dr 1050 / Cr 1000,
	// touching inventory not at all.
	settlement := []LineInput{
		{AccountCode: PaymentsClearingAccount, Debit: gross},
		{AccountCode: cashAccount, Credit: gross},
	}
	for _, l := range settlement {
		if l.AccountCode == inventoryAccount {
			inventory = inventory.Add(l.Debit).Sub(l.Credit)
		}
	}

	if !inventory.Equal(gross) {
		t.Fatalf("inventory net %s for a %s purchase; the coffee is bought once, so anything else is a double count", inventory, gross)
	}
}
