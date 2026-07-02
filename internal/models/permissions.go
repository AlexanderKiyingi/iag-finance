package models

// PermissionDescriptor is posted to iag-authentication at startup.
type PermissionDescriptor struct {
	Name        string
	Description string
}

// PermissionDescriptors returns finance RBAC codenames (gateway-aligned).
func PermissionDescriptors() []PermissionDescriptor {
	return []PermissionDescriptor{
		{Name: "finance.view_ledger", Description: "View chart of accounts, journal entries, and GL reports"},
		{Name: "finance.change_ledger", Description: "Create and post journal entries and chart accounts"},
		{Name: "finance.view_operations", Description: "View finance ops audit, inboxes, and prototype table rows"},
		{Name: "finance.change_operations", Description: "Append finance ops audit events and table rows"},
		{Name: "finance.view_own_ap", Description: "View own accounts-payable lines (vendor portal)"},
		{Name: "finance.view_own_payment", Description: "View own payment history (vendor portal)"},
		{Name: "finance.approve_tier1", Description: "Approve high-value journals/payments at tier 1 (lowest band)"},
		{Name: "finance.approve_tier2", Description: "Approve high-value journals/payments at tier 2 (mid band)"},
		{Name: "finance.approve_tier3", Description: "Approve high-value journals/payments at tier 3 (highest band)"},

		// Granular capability permissions (separation of duties). Each is also
		// satisfied by finance.change_ledger today so existing grants keep working;
		// remove change_ledger from a role to enforce only the narrow grant.
		{Name: "finance.manage_coa", Description: "Create/modify chart-of-accounts structure"},
		{Name: "finance.reverse_journal", Description: "Reverse a posted journal entry"},
		{Name: "finance.close_period", Description: "Close/reopen fiscal periods and run year-end close"},
		{Name: "finance.run_depreciation", Description: "Register fixed assets and run depreciation"},
		{Name: "finance.manage_fx", Description: "Manage exchange rates and run FX revaluation"},
		{Name: "finance.manage_tax", Description: "Manage VAT/GST tax codes"},
		{Name: "finance.submit_efris", Description: "Submit invoices to URA EFRIS (tax authority filing)"},
		{Name: "finance.manage_entities", Description: "Create/manage accounting entities"},
		{Name: "finance.manage_budgets", Description: "Set budgets"},
		{Name: "finance.manage_dimensions", Description: "Manage projects and cost centres"},
		{Name: "finance.issue_invoice", Description: "Create and issue customer invoices and recurring schedules"},
		{Name: "finance.collect_payment", Description: "Create and confirm payment intents (collect on AR)"},
		{Name: "finance.manage_provisions", Description: "Run ECL provisioning and write off / recover receivables"},
		{Name: "finance.manage_revenue", Description: "Manage revenue-recognition schedules and run recognition"},

		// Scoped read / cross-entity permissions.
		{Name: "finance.view_consolidated", Description: "View consolidated (cross-entity) financial reports"},
		{Name: "finance.cross_entity", Description: "Operate on a non-default accounting entity (X-Entity-Id)"},
		{Name: "finance.view_payroll", Description: "View payroll employee/leave mirror data"},
		{Name: "finance.view_own_ar", Description: "View own accounts-receivable lines (customer portal)"},

		// Farmer-payout domain (cherry intake / MoMo float). Defined in the auth
		// catalogue and surfaced in the finance UI; declared here so the finance
		// service's catalog stays the complete source of truth.
		{Name: "finance.view_farmerpayment", Description: "View farmer payments"},
		{Name: "finance.change_farmerpayment", Description: "Update farmer payments"},
	}
}
