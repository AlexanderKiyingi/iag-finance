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
		{"POST", "/integrations/banking/statements", ledgerWrite()},
		{"POST", "/integrations/banking/statements/:id/reconcile/auto", ledgerWrite()},
		{"POST", "/integrations/banking/lines/:lineId/match", ledgerWrite()},
		{"POST", "/integrations/banking/lines/:lineId/confirm", ledgerWrite()},
		{"POST", "/integrations/banking/lines/:lineId/reject", ledgerWrite()},
		{"POST", "/integrations/banking/sync", ledgerWrite()},

		// General ledger
		{"POST", "/chart-of-accounts", granular("finance.manage_coa")},
		{"PATCH", "/chart-of-accounts/:id", granular("finance.manage_coa")},
		{"DELETE", "/chart-of-accounts/:id", granular("finance.manage_coa")},
		{"POST", "/ledger/entries", ledgerWrite()},
		{"POST", "/ledger/entries/:id/post", ledgerWrite()},
		{"POST", "/ledger/entries/:id/reverse", granular("finance.reverse_journal")},
		{"POST", "/ledger/validate-posting", ledgerWrite()},
		{"POST", "/ledger/periods/:period/close", granular("finance.close_period")},
		{"POST", "/ledger/periods/:period/reopen", granular("finance.close_period")},
		{"POST", "/ledger/year-end/:year/close", granular("finance.close_period")},
		{"POST", "/fixed-assets", granular("finance.run_depreciation")},
		{"POST", "/fixed-assets/depreciation/run", granular("finance.run_depreciation")},

		// Approvals
		{"POST", "/approvals", ledgerWrite()},
		{"POST", "/approvals/:id/approve", ledgerWrite()},
		{"POST", "/approvals/:id/reject", ledgerWrite()},

		// FX / tax / entities / budgets / dimensions
		{"POST", "/fx/rates", granular("finance.manage_fx")},
		{"POST", "/fx/revalue", granular("finance.manage_fx")},
		{"POST", "/tax-codes", granular("finance.manage_tax")},
		{"POST", "/entities", granular("finance.manage_entities")},
		{"POST", "/budgets", granular("finance.manage_budgets")},
		{"POST", "/projects", granular("finance.manage_dimensions")},
		{"POST", "/cost-centers", granular("finance.manage_dimensions")},
		{"POST", "/customers", granular("finance.manage_dimensions")},
		{"POST", "/vendors", granular("finance.manage_dimensions")},

		// Billing / payments
		{"POST", "/billing/invoices", granular("finance.issue_invoice")},
		{"POST", "/billing/invoices/:id/issue", granular("finance.issue_invoice")},
		{"POST", "/billing/recurring", granular("finance.issue_invoice")},
		{"POST", "/payments/intents", granular("finance.collect_payment")},
		{"POST", "/payments/intents/:id/confirm", granular("finance.collect_payment")},

		// Legacy invoices
		{"POST", "/invoices", ledgerWrite()},
		{"PATCH", "/invoices/:no", ledgerWrite()},
		{"DELETE", "/invoices/:no", ledgerWrite()},

		// Legacy AP bills (AP counterpart of /invoices)
		{"POST", "/bills", ledgerWrite()},
		{"PATCH", "/bills/:no", ledgerWrite()},
		{"DELETE", "/bills/:no", ledgerWrite()},

		// AR / AP
		{"POST", "/ar/items", ledgerWrite()},
		{"POST", "/ar/items/:id/payments", ledgerWrite()},
		{"POST", "/ar/credit-notes", ledgerWrite()},
		{"POST", "/ar/debit-notes", ledgerWrite()},
		{"POST", "/ap/credit-notes", ledgerWrite()},
		{"POST", "/ap/debit-notes", ledgerWrite()},
		{"POST", "/ap/items", ledgerWrite()},
		{"POST", "/ap/items/:id/payments", ledgerWrite()},

		// Payroll
		{"POST", "/payroll/runs", ledgerWrite()},

		// Ops audit / prototype tables
		{"POST", "/audit/events", opsWrite()},
		{"POST", "/tables/:tableId/rows", opsWrite()},
	}
}
