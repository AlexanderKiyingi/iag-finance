package usersclient

import "testing"

func TestDeriveCustomerRef(t *testing.T) {
	ref := "CUST-1"
	bi := &BillingIdentity{LegalName: "IAG Coffee Ltd", FinanceCustomerRef: &ref}
	if got := DeriveCustomerRef(bi); got != "CUST-1" {
		t.Fatalf("explicit ref = %q, want CUST-1", got)
	}
	bi.FinanceCustomerRef = nil
	if got := DeriveCustomerRef(bi); got != "IAG-COFFEE-LTD" {
		t.Fatalf("slug = %q, want IAG-COFFEE-LTD", got)
	}
}
