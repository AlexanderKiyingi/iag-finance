package models

func SeedExpenses() []Expense {
	return []Expense{
		{ID: "EXP-1042", Date: "2026-05-12", Vendor: "UMEME", Category: "Utilities — Electricity", Amount: 845000, Status: "Approved"},
		{ID: "EXP-1041", Date: "2026-05-10", Vendor: "Wycliffe Wabwire", Category: "Green Bean", Amount: 4200000, Status: "Pending"},
	}
}

func SeedWorkers() []Worker {
	return []Worker{
		{ID: "W-001", Name: "Menelik Habtemariam", Role: "Master Admin", Dept: "Finance", Status: "Active"},
		{ID: "W-002", Name: "Perez Akanyijuka", Role: "Supervisor", Dept: "Finance", Status: "Active"},
	}
}

func SeedFinanceUsers() []FinanceUser {
	return []FinanceUser{
		{ID: "u-mk", Name: "Menelik Habtemariam", Email: "menelik@iagcoffee.com", Role: "Master Admin", Status: "Active", Billable: "Yes"},
		{ID: "u-cfo", Name: "M. Kassim (CFO)", Email: "kassim@iagcoffee.com", Role: "Company Admin", Status: "Active", Billable: "Yes"},
	}
}

func SeedNotifications() []Notification {
	return []Notification{
		{ID: "N-1", Title: "EFRIS window closing", Body: "Submit INV-1042 before 17:00", Time: "2026-05-13 09:00", Read: false},
		{ID: "N-2", Title: "Bank feed", Body: "12 transactions to review", Time: "2026-05-13 08:30", Read: false},
	}
}

func SeedBudgets() []Budget {
	return []Budget{
		{ID: "B-2026-Q2", Name: "Operations Q2", Period: "2026-Q2", Amount: 120000000, Spent: 84200000},
	}
}

func SeedJournals() []JournalEntry {
	return []JournalEntry{
		{ID: "JE-0043", Date: "2026-05-12", Memo: "May payroll posting", Debit: 53900000, Credit: 53900000, Status: "Awaiting CEO"},
	}
}

func SeedInventory() []InventoryItem {
	return []InventoryItem{
		{Code: "BGS-AA", Name: "Bugisu AA", Category: "Green Bean", Qty: 12400, Rate: 17800},
		{Code: "HKA-CL", Name: "HARAKA Instant Classic", Category: "Finished", Qty: 2200, Rate: 66700},
	}
}

func SeedTaxes() []TaxRecord {
	return []TaxRecord{
		{ID: "T-VAT-MAY", Period: "May 2026", Type: "VAT Output (EFRIS)", Amount: 19880000, Status: "Filed"},
		{ID: "T-WHT-MAY", Period: "May 2026", Type: "Withholding", Amount: 4200000, Status: "Due"},
	}
}

func DefaultSettings() map[string]string {
	return map[string]string{
		"currency": "UGX",
		"entity":   "Africa Coffee Park",
		"theme":    "dark",
	}
}

func NewPersistedState() *PersistedState {
	return &PersistedState{
		Session:       DefaultSession(),
		Invoices:      SeedInvoices(),
		BankAccounts:  SeedBankAccounts(),
		BankTx:        SeedBankTx(),
		FixedAssets:   SeedFixedAssets(),
		Approvals:     SeedApprovals(),
		AuditLog:      SeedAuditLog(),
		Expenses:      SeedExpenses(),
		Workers:       SeedWorkers(),
		Users:         SeedFinanceUsers(),
		Notifications: SeedNotifications(),
		Budgets:       SeedBudgets(),
		Journals:      SeedJournals(),
		Inventory:     SeedInventory(),
		Taxes:         SeedTaxes(),
		Settings:      DefaultSettings(),
		NextInv:       1043,
	}
}
