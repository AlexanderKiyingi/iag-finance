package handlers

import (
	"testing"

	"github.com/shopspring/decimal"
)

// The POS endpoint is the only path where an outside system hands finance a
// posting rather than a fact, so the posting shape is this service's
// responsibility to get right. These pin it without a ledger or a database.

func sumSides(t *testing.T, r posReceipt) (debit, credit decimal.Decimal, byAccount map[string]decimal.Decimal) {
	t.Helper()
	lines, err := posLines(r)
	if err != nil {
		t.Fatalf("posLines: %v", err)
	}
	byAccount = map[string]decimal.Decimal{}
	debit, credit = decimal.Zero, decimal.Zero
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
		byAccount[l.AccountCode] = byAccount[l.AccountCode].Add(l.Debit).Sub(l.Credit)
	}
	return debit, credit, byAccount
}

func TestPOSPostingsBalance(t *testing.T) {
	cases := []struct {
		name string
		r    posReceipt
	}{
		{"cash sale with vat", posReceipt{Gross: "25000", VAT: "3814", Tender: "cash"}},
		{"cash sale without vat", posReceipt{Gross: "25000", Tender: "cash"}},
		{"card sale with vat", posReceipt{Gross: "118000", VAT: "18000", Tender: "card"}},
		{"return with vat", posReceipt{Gross: "25000", VAT: "3814", Tender: "cash", Type: "return"}},
		{"return without vat", posReceipt{Gross: "25000", Tender: "cash", Type: "return"}},
		{"fractional amounts", posReceipt{Gross: "1234.56", VAT: "188.32", Tender: "mobile"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			debit, credit, _ := sumSides(t, tc.r)
			if !debit.Equal(credit) {
				t.Fatalf("debits %s != credits %s", debit, credit)
			}
			if debit.IsZero() {
				t.Fatal("posted nothing")
			}
		})
	}
}

func TestPOSCashSaleDebitsTheDrawerAndSplitsVAT(t *testing.T) {
	_, _, acct := sumSides(t, posReceipt{Gross: "25000", VAT: "5000", Tender: "cash"})

	if got := acct[posAccountCash]; !got.Equal(decimal.NewFromInt(25000)) {
		t.Errorf("cash %s = %s, want 25000 debit", posAccountCash, got)
	}
	// Revenue and VAT are credits, so they read negative in this net view.
	if got := acct[posAccountRevenue]; !got.Equal(decimal.NewFromInt(-20000)) {
		t.Errorf("revenue = %s, want 20000 credit (gross less VAT)", got)
	}
	if got := acct[posAccountOutputVAT]; !got.Equal(decimal.NewFromInt(-5000)) {
		t.Errorf("output VAT = %s, want 5000 credit", got)
	}
}

// Card and mobile money are not in the drawer — the acquirer owes them until it
// settles, which is what the clearing account is for. Booking them as cash
// would overstate the till and leave the later settlement with nothing to
// relieve.
func TestPOSCardAndMobileGoToClearingNotCash(t *testing.T) {
	for _, tender := range []string{"card", "mobile", "momo", "mobile_money"} {
		t.Run(tender, func(t *testing.T) {
			_, _, acct := sumSides(t, posReceipt{Gross: "10000", Tender: tender})
			if _, ok := acct[posAccountCash]; ok {
				t.Errorf("%s receipt touched cash %s", tender, posAccountCash)
			}
			if got := acct[posAccountClearing]; !got.Equal(decimal.NewFromInt(10000)) {
				t.Errorf("clearing = %s, want 10000 debit", got)
			}
		})
	}
}

// An unrecognised tender is treated as cash: the money is in the drawer unless
// the till says otherwise, and silently dropping the receipt would be worse.
func TestPOSUnknownTenderFallsBackToCash(t *testing.T) {
	_, _, acct := sumSides(t, posReceipt{Gross: "500", Tender: "voucher"})
	if got := acct[posAccountCash]; !got.Equal(decimal.NewFromInt(500)) {
		t.Errorf("cash = %s, want the unknown tender to fall back to cash", got)
	}
}

func TestPOSReturnReversesTheSaleExactly(t *testing.T) {
	sale := posReceipt{Gross: "25000", VAT: "5000", Tender: "cash"}
	ret := posReceipt{Gross: "25000", VAT: "5000", Tender: "cash", Type: "return"}

	_, _, saleAcct := sumSides(t, sale)
	_, _, retAcct := sumSides(t, ret)

	for code, saleNet := range saleAcct {
		retNet, ok := retAcct[code]
		if !ok {
			t.Errorf("return does not touch %s, which the sale did", code)
			continue
		}
		if !retNet.Equal(saleNet.Neg()) {
			t.Errorf("account %s: sale %s, return %s — a return must be the exact reverse",
				code, saleNet, retNet)
		}
	}
	if len(retAcct) != len(saleAcct) {
		t.Errorf("return touches %d accounts, sale touches %d", len(retAcct), len(saleAcct))
	}
}

func TestPOSRejectsAmountsThatCannotBeBooked(t *testing.T) {
	cases := []struct {
		name string
		r    posReceipt
	}{
		{"zero gross", posReceipt{Gross: "0", Tender: "cash"}},
		{"negative gross", posReceipt{Gross: "-100", Tender: "cash"}},
		{"unparseable gross", posReceipt{Gross: "abc", Tender: "cash"}},
		{"unparseable vat", posReceipt{Gross: "100", VAT: "abc", Tender: "cash"}},
		{"negative vat", posReceipt{Gross: "100", VAT: "-5", Tender: "cash"}},
		// VAT equal to or above gross would leave revenue zero or negative —
		// the till has sent the two the wrong way round.
		{"vat equals gross", posReceipt{Gross: "100", VAT: "100", Tender: "cash"}},
		{"vat exceeds gross", posReceipt{Gross: "100", VAT: "120", Tender: "cash"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := posLines(tc.r); err == nil {
				t.Fatal("expected the receipt to be refused")
			}
		})
	}
}

// A refund larger than the original sale is a legitimate business event this
// endpoint should not silently reshape — it books as given, and reconciliation
// is a reporting concern, not an ingest one.
func TestPOSDoesNotSecondGuessRefundSize(t *testing.T) {
	debit, credit, _ := sumSides(t, posReceipt{Gross: "999999", Tender: "cash", Type: "return"})
	if !debit.Equal(credit) {
		t.Fatalf("large refund did not balance: %s vs %s", debit, credit)
	}
}
