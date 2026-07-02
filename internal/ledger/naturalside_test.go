package ledger

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestViolatesNaturalSide(t *testing.T) {
	t.Parallel()
	d := decimal.NewFromInt(100)
	z := decimal.Zero

	cases := []struct {
		name        string
		accountType string
		debit       decimal.Decimal
		credit      decimal.Decimal
		want        bool
	}{
		// Debit-normal accounts: a credit is unnatural.
		{"asset debited (ok)", "asset", d, z, false},
		{"asset credited (violation)", "asset", z, d, true},
		{"expense debited (ok)", "expense", d, z, false},
		{"expense credited (violation)", "expense", z, d, true},
		// Credit-normal accounts: a debit is unnatural.
		{"liability credited (ok)", "liability", z, d, false},
		{"liability debited (violation)", "liability", d, z, true},
		{"equity credited (ok)", "equity", z, d, false},
		{"revenue debited (violation)", "revenue", d, z, true},
		// Unknown type is never restricted.
		{"unknown type", "other", d, z, false},
	}
	for _, c := range cases {
		if got := violatesNaturalSide(c.accountType, c.debit, c.credit); got != c.want {
			t.Errorf("%s: violatesNaturalSide(%s) = %v, want %v", c.name, c.accountType, got, c.want)
		}
	}
}

func TestPeriodEnd(t *testing.T) {
	t.Parallel()
	end, err := periodEnd("2026-02")
	if err != nil {
		t.Fatalf("periodEnd: %v", err)
	}
	if got := end.Format("2006-01-02"); got != "2026-02-28" {
		t.Errorf("periodEnd(2026-02) = %s, want 2026-02-28", got)
	}
	if _, err := periodEnd("bad"); err == nil {
		t.Error("expected error for malformed period")
	}
}
