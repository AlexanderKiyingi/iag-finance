package consumer

import (
	"errors"
	"testing"
)

// A malformed amount used to be indistinguishable from no amount: parseAmount
// returned zero for both, and every handler treats zero as "nothing to book" by
// returning nil — which commits the message as handled. A financial event with
// an unreadable figure was therefore discarded silently, never retried and
// never sent to the DLQ.

func TestStrictParseAcceptsUsableAmounts(t *testing.T) {
	for _, in := range []string{"1", "0.01", "1234.56", " 42 ", "1000000"} {
		got, ok, err := parseAmountStrict(in)
		if err != nil || !ok {
			t.Errorf("%q: ok=%v err=%v, want a usable amount", in, ok, err)
			continue
		}
		if !got.IsPositive() {
			t.Errorf("%q parsed to %s, want a positive amount", in, got)
		}
	}
}

func TestStrictParseTreatsAnEmptyAmountAsNothingToBook(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, ok, err := parseAmountStrict(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v — an absent amount is a legitimate no-op", in, err)
		}
		if ok || !got.IsZero() {
			t.Errorf("%q: ok=%v amount=%s, want a no-op", in, ok, got)
		}
	}
}

// These are the values that were being thrown away. A thousands separator or a
// trailing currency code is exactly what a careless producer sends, and it must
// reach the DLQ rather than vanish.
func TestStrictParseRejectsAmountsItCannotRead(t *testing.T) {
	for _, in := range []string{"1,234.56", "12.5 USD", "abc", "UGX 500", "1.2.3", "--5"} {
		_, ok, err := parseAmountStrict(in)
		if err == nil {
			t.Errorf("%q: accepted (ok=%v); an unreadable amount must be an error, not a silent drop", in, ok)
			continue
		}
		if !errors.Is(err, errUnparseableAmount) {
			t.Errorf("%q: error %v does not wrap errUnparseableAmount", in, err)
		}
	}
}

// Zero and negative amounts are refused too: a payable of zero books nothing,
// and a negative one is a producer bug that should surface rather than reverse
// an entry nobody asked to reverse.
func TestStrictParseRejectsNonPositiveAmounts(t *testing.T) {
	for _, in := range []string{"0", "0.00", "-1", "-0.01"} {
		if _, _, err := parseAmountStrict(in); err == nil {
			t.Errorf("%q: accepted; a non-positive amount should be reported", in)
		}
	}
}

// The lenient reading stays for paths where zero genuinely means skip, so its
// behaviour is pinned rather than left to drift.
func TestLenientParseStillCollapsesEverythingUnusableToZero(t *testing.T) {
	for _, in := range []string{"", "abc", "0", "-5", "1,234"} {
		if got := parseAmount(in); !got.IsZero() {
			t.Errorf("parseAmount(%q) = %s, want zero", in, got)
		}
	}
	if got := parseAmount("7.50"); got.String() != "7.5" {
		t.Errorf("parseAmount(7.50) = %s, want 7.5", got)
	}
}
