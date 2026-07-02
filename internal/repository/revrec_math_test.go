package repository

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestRatableSlicesSumToTotal(t *testing.T) {
	t.Parallel()
	// 100 / 3 must split without losing a cent to rounding.
	total := decimal.RequireFromString("100.00")
	slices := ratableSlices(total, 3)
	if len(slices) != 3 {
		t.Fatalf("want 3 slices, got %d", len(slices))
	}
	sum := decimal.Zero
	for _, s := range slices {
		sum = sum.Add(s)
	}
	if !sum.Equal(total) {
		t.Errorf("slices sum to %s, want %s", sum, total)
	}
	// The remainder is absorbed by the last slice.
	if slices[0].String() != "33.33" || slices[2].String() != "33.34" {
		t.Errorf("unexpected split: %s / %s / %s", slices[0], slices[1], slices[2])
	}
}

func TestRatableSlicesSinglePeriod(t *testing.T) {
	t.Parallel()
	total := decimal.RequireFromString("500.00")
	slices := ratableSlices(total, 1)
	if len(slices) != 1 || !slices[0].Equal(total) {
		t.Errorf("single period should equal total, got %v", slices)
	}
}

func TestAddMonths(t *testing.T) {
	t.Parallel()
	if got := addMonths("2026-11", 3); got != "2027-02" {
		t.Errorf("addMonths(2026-11,3) = %s, want 2027-02", got)
	}
	if got := addMonths("2026-01", 0); got != "2026-01" {
		t.Errorf("addMonths(2026-01,0) = %s, want 2026-01", got)
	}
}

func TestPresentValueDiscounts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	settle := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC) // ~2 years out
	estimate := decimal.NewFromInt(1000)
	rate := decimal.RequireFromString("0.10")

	pv := presentValue(estimate, rate, &settle, now)
	// 1000 / 1.1^2 ≈ 826.45; assert it discounts to well below the estimate.
	if !pv.LessThan(estimate) {
		t.Errorf("present value %s should be below estimate %s", pv, estimate)
	}
	if pv.LessThan(decimal.NewFromInt(800)) || pv.GreaterThan(decimal.NewFromInt(840)) {
		t.Errorf("present value %s outside expected ~826 band", pv)
	}

	// No rate or no settlement date → undiscounted.
	if got := presentValue(estimate, decimal.Zero, &settle, now); !got.Equal(estimate) {
		t.Errorf("zero rate should not discount, got %s", got)
	}
	if got := presentValue(estimate, rate, nil, now); !got.Equal(estimate) {
		t.Errorf("nil settlement should not discount, got %s", got)
	}
}
