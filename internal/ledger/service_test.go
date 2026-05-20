package ledger

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestValidateBalance(t *testing.T) {
	t.Parallel()

	balanced := []LineInput{
		{AccountCode: "1100", Debit: decimal.NewFromInt(100)},
		{AccountCode: "4000", Credit: decimal.NewFromInt(100)},
	}
	if err := validateBalance(balanced); err != nil {
		t.Fatalf("expected balanced entry: %v", err)
	}

	unbalanced := []LineInput{
		{AccountCode: "1100", Debit: decimal.NewFromInt(100)},
		{AccountCode: "4000", Credit: decimal.NewFromInt(50)},
	}
	if err := validateBalance(unbalanced); err != ErrUnbalancedEntry {
		t.Fatalf("expected ErrUnbalancedEntry, got %v", err)
	}
}
