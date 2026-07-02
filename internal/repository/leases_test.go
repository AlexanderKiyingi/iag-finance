package repository

import (
	"testing"

	"github.com/shopspring/decimal"
)

// The lease amortization schedule must satisfy the IFRS 16 accounting
// invariants regardless of rate, so the periodic run always books balanced,
// self-closing entries.
func TestBuildLeaseSchedule(t *testing.T) {
	cases := []struct {
		name    string
		payment string
		rate    string
		term    int
	}{
		{"interest-free", "1000", "0", 12},
		{"12pct-annual", "1000", "0.12", 24},
		{"single-period", "5000", "0.10", 1},
		{"long-low-rate", "250.50", "0.05", 36},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payment := decimal.RequireFromString(tc.payment)
			rate := decimal.RequireFromString(tc.rate)
			pv, rows := buildLeaseSchedule(payment, rate, tc.term, "2026-01")

			if len(rows) != tc.term {
				t.Fatalf("expected %d schedule rows, got %d", tc.term, len(rows))
			}
			if pv.LessThanOrEqual(decimal.Zero) {
				t.Fatalf("present value must be positive, got %s", pv)
			}
			// With a positive discount rate, PV must be strictly less than the sum
			// of undiscounted payments; interest-free, they are equal.
			gross := payment.Mul(decimal.NewFromInt(int64(tc.term)))
			if rate.IsZero() && !pv.Equal(gross) {
				t.Fatalf("interest-free PV should equal gross %s, got %s", gross, pv)
			}
			if rate.IsPositive() && !pv.LessThan(gross) {
				t.Fatalf("discounted PV %s should be < gross %s", pv, gross)
			}

			// First opening liability equals the recognised liability (= PV).
			if !rows[0].opening.Equal(pv) {
				t.Fatalf("first opening %s should equal PV %s", rows[0].opening, pv)
			}

			sumDepr := decimal.Zero
			for i, r := range rows {
				// Every period: payment = interest + principal.
				if !r.payment.Equal(r.interest.Add(r.principal)) {
					t.Fatalf("row %d: payment %s != interest %s + principal %s", i, r.payment, r.interest, r.principal)
				}
				// closing = opening - principal.
				if !r.closing.Equal(r.opening.Sub(r.principal)) {
					t.Fatalf("row %d: closing %s != opening %s - principal %s", i, r.closing, r.opening, r.principal)
				}
				sumDepr = sumDepr.Add(r.depreciation)
			}
			// The liability closes at exactly zero at the end of the term.
			last := rows[len(rows)-1]
			if !last.closing.Equal(decimal.Zero) {
				t.Fatalf("final closing liability should be zero, got %s", last.closing)
			}
			// Accumulated depreciation equals the ROU asset (= PV).
			if !sumDepr.Equal(pv) {
				t.Fatalf("sum of depreciation %s should equal ROU %s", sumDepr, pv)
			}
		})
	}
}
