package repository

import (
	"testing"

	"github.com/shopspring/decimal"
)

// The investment/equity elimination must balance for any ownership level: the
// asset-side adjustments and the equity-side adjustments each net to
// −(ownership × subsidiary equity), so a consolidated balance sheet stays
// balanced after the elimination.
func TestInvestmentEquityRows(t *testing.T) {
	dec := decimal.RequireFromString
	cases := []struct {
		name              string
		invest, equity    string
		ownership         string
		wantNCI, wantGood string
	}{
		{"wholly-owned at book", "80", "80", "1.0", "0.00", "0.00"},
		{"wholly-owned with goodwill", "100", "80", "1.0", "0.00", "20.00"},
		{"80pct with NCI and goodwill", "90", "100", "0.8", "20.00", "10.00"},
		{"bargain purchase", "50", "80", "1.0", "0.00", "-30.00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invest, equity, ownership := dec(tc.invest), dec(tc.equity), dec(tc.ownership)
			rows, nci, goodwill := investmentEquityRows("SUB", invest, equity, ownership)

			if nci.StringFixed(2) != tc.wantNCI {
				t.Fatalf("NCI = %s, want %s", nci.StringFixed(2), tc.wantNCI)
			}
			if goodwill.StringFixed(2) != tc.wantGood {
				t.Fatalf("goodwill = %s, want %s", goodwill.StringFixed(2), tc.wantGood)
			}

			// The adjustments must balance: Σ asset rows == Σ equity rows.
			assetSum, equitySum := decimal.Zero, decimal.Zero
			for _, row := range rows {
				amt := dec(row.Amount)
				switch row.AccountType {
				case "asset":
					assetSum = assetSum.Add(amt)
				case "equity":
					equitySum = equitySum.Add(amt)
				default:
					t.Fatalf("unexpected account type %q", row.AccountType)
				}
			}
			if !assetSum.Equal(equitySum) {
				t.Fatalf("unbalanced: asset adj %s != equity adj %s", assetSum, equitySum)
			}
			// Both net to −(ownership × equity).
			want := equity.Mul(ownership).Neg()
			if !assetSum.Equal(want) {
				t.Fatalf("asset adjustment %s, want %s", assetSum, want)
			}
		})
	}
}
