package models

func SeedInvoices() []Invoice {
	return []Invoice{
		{No: "INV-1042", Date: "2026-05-08", Due: "2026-06-07", Customer: "Matsuri Coffee Co.", Total: 33800000, Balance: 33800000, Status: "Open"},
		{No: "INV-1041", Date: "2026-05-02", Due: "2026-06-01", Customer: "Amsterdam Specialty", Total: 28950000, Balance: 28950000, Status: "Open"},
		{No: "INV-1040", Date: "2026-04-22", Due: "2026-05-22", Customer: "Kaffee Haus Bergmann", Total: 19500000, Balance: 6500000, Status: "Partial"},
		{No: "INV-1039", Date: "2026-04-12", Due: "2026-05-12", Customer: "Union Hand-Roasted", Total: 12450000, Balance: 12450000, Status: "Overdue"},
		{No: "INV-1038", Date: "2026-04-05", Due: "2026-05-05", Customer: "Seoul Bean Lab", Total: 8800000, Balance: 8800000, Status: "Overdue"},
		{No: "INV-1037", Date: "2026-03-28", Due: "2026-04-27", Customer: "Emirates Coffee Group", Total: 21000000, Balance: 0, Status: "Paid"},
		{No: "INV-1036", Date: "2026-03-15", Due: "2026-04-14", Customer: "Matsuri Coffee Co.", Total: 19800000, Balance: 0, Status: "Paid"},
	}
}

func SeedBankAccounts() []BankAccount {
	return []BankAccount{
		{Name: "Stanbic UGX Current", Balance: 184200000, InBooks: 178650000, Review: 12, Type: "checking"},
		{Name: "Stanbic USD Export", Balance: 42800, InBooks: 41200, Review: 4, Type: "checking", Currency: "USD"},
		{Name: "Centenary UGX", Balance: 38400000, InBooks: 38400000, Review: 0, Type: "checking"},
	}
}

func SeedBankTx() []BankTx {
	spent845 := 845000.0
	spent4200 := 4200000.0
	spent45 := 45000.0
	spent320 := 320000.0
	recv19800 := 19800000.0
	recv12300 := 12300000.0
	spent180 := 180000.0
	spent2850 := 2850000.0
	spent3150 := 3150000.0
	spent500 := 500000.0
	m1 := "Invoice INV-1036 · 03/15/2026 · UGX 19,800,000"
	m2 := "Bill BILL-0089 · 05/02/2026 · UGX 4,200,000"
	m3 := "Bill BILL-0088 · 04/30/2026 · UGX 3,150,000"
	m4 := "2 records found"
	return []BankTx{
		{Date: "2026-05-12", Desc: "MATSURI COFFEE WIRE", Payee: "Matsuri Coffee Co.", Category: "Match: INV-1036 UGX 19,800,000", Received: &recv19800, Action: "match", Matched: &m1},
		{Date: "2026-05-11", Desc: "UMEME ELECTRICITY", Payee: "UMEME", Category: "Utilities — Electricity", Spent: &spent845, Action: "add"},
		{Date: "2026-05-10", Desc: "FRM-001 PAYMENT", Payee: "Wycliffe Wabwire (FRM-001)", Category: "Match: BILL-0089 UGX 4,200,000", Spent: &spent4200, Action: "match", Matched: &m2},
		{Date: "2026-05-09", Desc: "STANBIC ACCT FEE", Payee: "Stanbic Bank Uganda", Category: "Bank Fees", Spent: &spent45, Action: "add"},
		{Date: "2026-05-08", Desc: "FUEL TOTAL ENERGIES", Payee: "TotalEnergies Uganda", Category: "Uncategorized Expense", Spent: &spent320, Action: "add"},
		{Date: "2026-05-07", Desc: "AMSTERDAM SPC WIRE", Payee: "Amsterdam Specialty", Category: "Match: 2 records found", Received: &recv12300, Action: "view", Matched: &m4},
		{Date: "2026-05-06", Desc: "NWSC WATER", Payee: "NWSC", Category: "Utilities — Water", Spent: &spent180, Action: "add"},
		{Date: "2026-05-05", Desc: "COFFEE EQUIP CO INV", Payee: "Coffee Processing Equipment Co.", Category: "Factory Maintenance", Spent: &spent2850, Action: "add"},
		{Date: "2026-05-04", Desc: "FRM-002 PAYMENT", Payee: "Agnes Chemutai (FRM-002)", Category: "Match: BILL-0088 UGX 3,150,000", Spent: &spent3150, Action: "match", Matched: &m3},
		{Date: "2026-05-03", Desc: "UNKNOWN TRANSFER", Payee: "", Category: "Uncategorized Expense", Spent: &spent500, Action: "add"},
	}
}

func SeedFixedAssets() []FixedAsset {
	return []FixedAsset{
		{Tag: "FA-2024-001", Name: "Probat P30 Roaster Drum 30kg", Category: "Factory Equipment", Acq: "2024-08-15", Cost: 92000000, Useful: 10, Method: "Straight-line", Residual: 5000000, AccumDep: 13050000, NBV: 78950000, Location: "Roasting Plant · Bay 2", Custodian: "David Lubega", Status: "In service"},
		{Tag: "FA-2023-018", Name: "Penagos Cherry Pulper UC-1500", Category: "Factory Equipment", Acq: "2023-03-22", Cost: 38000000, Useful: 12, Method: "Straight-line", Residual: 2000000, AccumDep: 9000000, NBV: 29000000, Location: "Wet Mill · Stage 1", Custodian: "Sarah Akello", Status: "In service"},
		{Tag: "FA-2023-019", Name: "Mahlkönig Industrial Grinder", Category: "Factory Equipment", Acq: "2023-03-22", Cost: 24500000, Useful: 8, Method: "Straight-line", Residual: 1500000, AccumDep: 8625000, NBV: 15875000, Location: "Roasting Plant · Bay 1", Custodian: "David Lubega", Status: "In service"},
		{Tag: "FA-2022-007", Name: "Toyota Hilux 4WD · UAH 234X", Category: "Vehicle", Acq: "2022-06-10", Cost: 145000000, Useful: 7, Method: "Straight-line", Residual: 25000000, AccumDep: 65142857, NBV: 79857143, Location: "Field Operations", Custodian: "Perez Akanyijuka", Status: "In service"},
		{Tag: "FA-2022-008", Name: "Isuzu NQR Truck · UAH 902T", Category: "Vehicle", Acq: "2022-09-04", Cost: 220000000, Useful: 8, Method: "Straight-line", Residual: 40000000, AccumDep: 78750000, NBV: 141250000, Location: "Logistics Yard", Custodian: "Perez Akanyijuka", Status: "In service"},
		{Tag: "FA-2024-014", Name: "Cupping Lab Sample Roaster", Category: "Lab Equipment", Acq: "2024-01-18", Cost: 18400000, Useful: 8, Method: "Straight-line", Residual: 1000000, AccumDep: 3045833, NBV: 15354167, Location: "QC Lab", Custodian: "Sarah Akello", Status: "In service"},
		{Tag: "FA-2023-022", Name: "Factory Building · Wet Mill Wing", Category: "Building", Acq: "2023-01-15", Cost: 480000000, Useful: 40, Method: "Straight-line", Residual: 80000000, AccumDep: 23333333, NBV: 456666667, Location: "Africa Coffee Park", Custodian: "Nelson Tugume", Status: "In service"},
		{Tag: "FA-2025-003", Name: "Solar Array 80kW + Inverters", Category: "Building", Acq: "2025-02-10", Cost: 124000000, Useful: 20, Method: "Straight-line", Residual: 8000000, AccumDep: 7250000, NBV: 116750000, Location: "Africa Coffee Park · Roof", Custodian: "David Lubega", Status: "In service"},
	}
}

func SeedApprovals() []Approval {
	return []Approval{
		{ID: "AP-2026-058", Type: "Purchase Order", Subject: "Probat P30 roaster + installation", Status: "Awaiting CFO", Date: "2026-05-13", Amount: 42000000},
		{ID: "AP-2026-057", Type: "Bill", Subject: "NSSF May 2026 contribution", Status: "Awaiting CFO", Date: "2026-05-12", Amount: 8900000},
		{ID: "AP-2026-056", Type: "Journal Entry", Subject: "May 2026 payroll posting · JE-0043", Status: "Awaiting CEO", Date: "2026-05-12", Amount: 53900000},
	}
}

func SeedAuditLog() []AuditEntry {
	return []AuditEntry{
		{TS: "2026-05-13 09:34", User: "Menelik", Entity: "Invoice INV-1042", Action: "Created"},
		{TS: "2026-05-13 09:12", User: "Kassim", Entity: "Bill BILL-0089", Action: "Approved"},
		{TS: "2026-05-12 17:48", User: "Perez", Entity: "Bank Match", Action: "Matched transaction"},
		{TS: "2026-05-12 15:22", User: "Menelik", Entity: "Customer CUS-007", Action: "Edited"},
	}
}

func SeedDashBars() []DashBar {
	return []DashBar{
		{D: "Sun", Date: "2026-05-04", Sav: 8, Inc: 14, Exp: 5},
		{D: "Mon", Date: "2026-05-05", Sav: 12, Inc: 18, Exp: 7},
		{D: "Tue", Date: "2026-05-06", Sav: 10, Inc: 22, Exp: 9},
		{D: "Wed", Date: "2026-05-07", Sav: 24, Inc: 28, Exp: 4.6, Highlight: true},
		{D: "Thu", Date: "2026-05-08", Sav: 14, Inc: 17, Exp: 8},
		{D: "Fri", Date: "2026-05-09", Sav: 9, Inc: 12, Exp: 6},
		{D: "Sat", Date: "2026-05-10", Sav: 16, Inc: 19, Exp: 7.5},
	}
}

func SeedCompany() Company {
	return Company{
		Name:    "Inspire Africa Group",
		Trading: "Africa Coffee Park",
		Address: []string{"Africa Coffee Park", "Rwashamaire, Ntungamo", "Uganda"},
		Email:   "finance@iagcoffee.com",
		VAT:     "1234567890",
		EFRIS:   "EFRIS-IAG-2026",
	}
}

func DefaultSession() Session {
	return Session{
		UserID:      "u-finance",
		Email:       "finance@iag.africa",
		DisplayName: "Finance Team",
		Role:        "finance_admin",
		Entity:      "Africa Coffee Park",
	}
}
