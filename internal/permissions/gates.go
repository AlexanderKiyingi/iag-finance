package permissions

// Broad write permissions that satisfy the "any-of" gates. Granular gates also
// accept changeLedger so existing change_ledger grants keep working during the
// separation-of-duties rollout (mirrors internal/middleware/rbac.go).
const (
	changeLedger     = "finance.change_ledger"
	changeOperations = "finance.change_operations"
)

// RouteGate is the RBAC gate on one mutating finance endpoint (path within the
// /v1 router group). Permissions is an any-of set — holding any one satisfies
// the gate; a superuser bypasses. Both the router (to build middleware via
// middleware.Require) and the manifest (consumed by the frontend) read this
// table, so the gate map has a single source of truth and cannot drift.
type RouteGate struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Permissions []string `json:"permissions"`
}

// ledgerWrite / opsWrite return fresh slices matching the named middleware
// helpers; granular returns a specific permission plus the change_ledger fallback.
func ledgerWrite() []string      { return []string{changeLedger, changeOperations} }
func opsWrite() []string         { return []string{changeOperations, changeLedger} }
func granular(p string) []string { return []string{p, changeLedger} }

// RouteGates is the authoritative gate map for every mutating finance route.
// Keep in sync with internal/api/router.go — the gatedGroup registrar panics at
// startup, and TestRouteGatesMatchRouter fails, if the two diverge.
func RouteGates() []RouteGate {
	return []RouteGate{
		// Integrations
		{"POST", "/integrations/ura-efris/submit", granular("finance.submit_efris")},
		{"POST", "/integrations/banking/statements", granular("finance.manage_banking")},
		{"POST", "/integrations/banking/statements/:id/reconcile/auto", granular("finance.manage_banking")},
		{"POST", "/integrations/banking/lines/:lineId/match", granular("finance.manage_banking")},
		{"POST", "/integrations/banking/lines/:lineId/confirm", granular("finance.manage_banking")},
		{"POST", "/integrations/banking/lines/:lineId/reject", granular("finance.manage_banking")},
		{"POST", "/integrations/banking/sync", granular("finance.manage_banking")},

		// General ledger
		{"POST", "/chart-of-accounts", granular("finance.manage_coa")},
		{"PATCH", "/chart-of-accounts/:id", granular("finance.manage_coa")},
		{"DELETE", "/chart-of-accounts/:id", granular("finance.manage_coa")},
		{"POST", "/ledger/entries", granular("finance.create_journal")},
		{"POST", "/ledger/entries/:id/post", granular("finance.post_journal")},
		{"POST", "/ledger/entries/:id/reverse", granular("finance.reverse_journal")},
		{"DELETE", "/ledger/entries/:id", granular("finance.create_journal")},
		{"POST", "/ledger/validate-posting", granular("finance.create_journal")},
		{"POST", "/ledger/periods/:period/close", granular("finance.close_period")},
		{"POST", "/ledger/periods/:period/reopen", granular("finance.close_period")},
		{"POST", "/ledger/year-end/:year/close", granular("finance.close_period")},
		{"POST", "/fixed-assets", granular("finance.run_depreciation")},
		{"POST", "/intangible-assets", granular("finance.run_depreciation")},
		{"POST", "/fixed-assets/depreciation/run", granular("finance.run_depreciation")},
		{"POST", "/fixed-assets/impair", granular("finance.run_depreciation")},
		{"POST", "/fixed-assets/reverse-impairment", granular("finance.run_depreciation")},
		{"POST", "/fixed-assets/revalue", granular("finance.run_depreciation")},

		// Approvals
		{"POST", "/approvals", ledgerWrite()},
		{"POST", "/approvals/:id/approve", ledgerWrite()},
		{"POST", "/approvals/:id/reject", ledgerWrite()},

		// FX / tax / entities / budgets / dimensions
		{"POST", "/fx/rates", granular("finance.manage_fx")},
		{"POST", "/fx/revalue", granular("finance.manage_fx")},
		{"POST", "/fx/conversions", granular("finance.manage_fx")},
		{"POST", "/tax-codes", granular("finance.manage_tax")},
		{"POST", "/tax/reverse-charge", granular("finance.manage_tax")},
		{"POST", "/tax/withholding", granular("finance.manage_tax")},
		{"POST", "/entities", granular("finance.manage_entities")},
		{"PATCH", "/entities/:id/ownership", granular("finance.manage_entities")},
		{"POST", "/budgets", granular("finance.manage_budgets")},
		{"POST", "/projects", granular("finance.manage_dimensions")},
		{"POST", "/cost-centers", granular("finance.manage_dimensions")},
		{"DELETE", "/cost-centers/:id", granular("finance.manage_dimensions")},
		{"POST", "/customers", granular("finance.manage_dimensions")},
		{"POST", "/vendors", granular("finance.manage_dimensions")},

		// Billing / payments
		{"POST", "/billing/invoices", granular("finance.issue_invoice")},
		{"POST", "/billing/invoices/:id/issue", granular("finance.issue_invoice")},
		{"POST", "/billing/recurring", granular("finance.issue_invoice")},
		{"POST", "/billing/time-entries", granular("finance.issue_invoice")},
		{"POST", "/payments/intents", granular("finance.collect_payment")},
		{"POST", "/payments/intents/:id/confirm", granular("finance.collect_payment")},

		// Legacy invoices (AR)
		{"POST", "/invoices", granular("finance.manage_ar")},
		{"PATCH", "/invoices/:no", granular("finance.manage_ar")},
		{"DELETE", "/invoices/:no", granular("finance.manage_ar")},

		// Legacy AP bills (AP counterpart of /invoices)
		{"POST", "/bills", granular("finance.manage_ap")},
		{"PATCH", "/bills/:no", granular("finance.manage_ap")},
		{"DELETE", "/bills/:no", granular("finance.manage_ap")},

		// AR / AP
		{"POST", "/ar/items", granular("finance.manage_ar")},
		{"POST", "/ar/items/:id/payments", granular("finance.manage_ar")},
		{"POST", "/ar/invoices/:documentRef/email", granular("finance.issue_invoice")},
		{"POST", "/ar/late-fees", granular("finance.manage_ar")},
		{"POST", "/ar/credit-notes", granular("finance.manage_ar")},
		{"POST", "/ar/debit-notes", granular("finance.manage_ar")},
		{"POST", "/ap/credit-notes", granular("finance.manage_ap")},
		{"POST", "/ap/debit-notes", granular("finance.manage_ap")},
		{"POST", "/ap/items", granular("finance.manage_ap")},
		{"POST", "/ap/items/:id/payments", granular("finance.manage_ap")},

		// IFRS 9 provisioning / write-off / recovery
		{"POST", "/provisions/ecl-run", granular("finance.manage_provisions")},
		{"POST", "/provisions/write-off", granular("finance.manage_provisions")},
		{"POST", "/provisions/recover", granular("finance.manage_provisions")},

		// IFRS 15 revenue recognition
		{"POST", "/revenue/schedules", granular("finance.manage_revenue")},
		{"POST", "/revenue/recognition-run", granular("finance.manage_revenue")},
		{"POST", "/revenue/obligations/:id/satisfy", granular("finance.manage_revenue")},
		{"POST", "/revenue/accrue", granular("finance.manage_revenue")},

		// IAS 1 matching — prepaid-expense amortization
		{"POST", "/prepayments", granular("finance.manage_prepayments")},
		{"POST", "/prepayments/amortization-run", granular("finance.manage_prepayments")},

		// IFRS 16 leases
		{"POST", "/leases", granular("finance.manage_leases")},
		{"POST", "/leases/run", granular("finance.manage_leases")},

		// IAS 12 income taxes (reuses the tax-management permission)
		{"POST", "/income-tax/current-run", granular("finance.manage_tax")},
		{"POST", "/income-tax/deferred", granular("finance.manage_tax")},

		// IAS 37 provisions
		{"POST", "/provisions/liability/recognize", granular("finance.manage_provisions")},
		{"POST", "/provisions/liability/unwind", granular("finance.manage_provisions")},
		{"POST", "/provisions/liability/utilize", granular("finance.manage_provisions")},
		{"POST", "/provisions/liability/remeasure", granular("finance.manage_provisions")},
		{"POST", "/provisions/liability/reverse", granular("finance.manage_provisions")},

		// Three-way match
		{"POST", "/procurement/match-check", ledgerWrite()},
		{"POST", "/procurement/match-exceptions/:id/resolve", ledgerWrite()},
		{"POST", "/procurement/match-variance/write-off", ledgerWrite()},

		// Payroll
		{"POST", "/payroll/runs", ledgerWrite()},

		// Ops audit / prototype tables
		{"POST", "/audit/events", opsWrite()},
		{"POST", "/tables/:tableId/rows", opsWrite()},
	}
}
