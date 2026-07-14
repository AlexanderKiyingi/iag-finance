package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/authclient"
	"github.com/iag-finance/backend/internal/chainaudit"
	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/db"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/handlers"
	"github.com/iag-finance/backend/internal/integrations"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/middleware"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/iag-finance/backend/internal/tablerows"
	"github.com/iag-finance/backend/internal/usersclient"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type RouterDeps struct {
	Config       config.Config
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	Verifier     *authclient.Verifier
	Ledger       *ledger.Service
	AuditLog     *auditlog.Service
	Events       *events.Bus
	Integrations *integrations.Registry
}

func NewRouter(deps RouterDeps) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), middleware.SecurityHeaders())

	corsCfg := cors.Config{
		AllowOrigins:     deps.Config.CORSAllowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Idempotency-Key", "X-Correlation-Id", "X-Request-Id"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * 3600,
	}
	// Only fall back to a localhost origin outside production. With
	// AllowCredentials=true a localhost default must never leak into a prod
	// deployment that forgot to set CORS_ALLOW_ORIGINS.
	if len(corsCfg.AllowOrigins) == 0 && deps.Config.Environment != "production" {
		corsCfg.AllowOrigins = []string{"http://localhost:3000"}
	}
	router.Use(cors.New(corsCfg))

	ops := &handlers.Handlers{
		Cfg:        deps.Config,
		DB:         &db.PoolHealth{Pool: deps.Pool},
		ChainAudit: &chainaudit.Store{Pg: deps.Pool, Redis: deps.Redis},
		Tables:     &tablerows.Store{Pg: deps.Pool},
		Redis:      deps.Redis,
	}

	users := usersclient.New(usersclient.Config{
		BaseURL:         deps.Config.UsersAPIURL,
		TokenURL:        deps.Config.AuthTokenURL,
		ServiceClientID: deps.Config.ServiceClientID,
		ServiceSecret:   deps.Config.ServiceClientSecret,
	})

	api := &handlers.API{
		Ledger:          deps.Ledger,
		Audit:           deps.AuditLog,
		DB:              &db.PoolHealth{Pool: deps.Pool},
		ConsumerEnabled: deps.Config.EnableConsumer,
		Events:          deps.Events,
		Integrations:    deps.Integrations,
		Cfg:             deps.Config,
		Users:           users,
		Repo:            newConfiguredRepo(deps),
	}

	router.GET("/health", ops.Health)
	router.GET("/ready", ops.Ready)

	// Realtime channel for the SPA. Registered outside the v1 group because a
	// browser WebSocket cannot send an Authorization header — the client
	// authenticates with an {type:"auth",token} frame instead (see WSHandler).
	ws := &handlers.WSHandler{Verifier: deps.Verifier, Repo: api.Repo}
	router.GET("/v1/ws/events", ws.Events)

	principal := middleware.Principal(deps.Verifier)
	ledgerRead := middleware.RequireLedgerRead()
	opsRead := middleware.RequireOperationsRead()
	viewPayroll := middleware.Require("finance.view_payroll", "finance.view_ledger", "finance.view_operations")

	v1 := router.Group("/v1", principal, middleware.EntityContext(), middleware.AuditLog(deps.AuditLog))
	{
		// Mutating routes are gated from the declarative permission table
		// (permissions.RouteGates()) via the registrar, which panics at startup
		// for any mutating route lacking a gate. Reads keep explicit read scope.
		w := newGatedGroup(v1)

		// Integrations
		v1.GET("/integrations/ura-efris", ledgerRead, api.URAStatus)
		w.POST("/integrations/ura-efris/submit", api.SubmitEFRIS)
		v1.GET("/integrations/banking", ledgerRead, api.BankingStatus)
		w.POST("/integrations/banking/statements", api.ImportBankStatement)
		v1.GET("/integrations/banking/statements/:id/lines", ledgerRead, api.ListStatementLines)
		w.POST("/integrations/banking/statements/:id/reconcile/auto", api.AutoReconcileStatement)
		w.POST("/integrations/banking/lines/:lineId/match", api.MatchStatementLine)
		w.POST("/integrations/banking/lines/:lineId/confirm", api.ConfirmStatementLine)
		w.POST("/integrations/banking/lines/:lineId/reject", api.RejectStatementLine)
		w.POST("/integrations/banking/sync", api.SyncBankFeed)

		// General ledger (accounting)
		v1.GET("/chart-of-accounts", ledgerRead, api.ListChartOfAccounts)
		w.POST("/chart-of-accounts", api.CreateChartAccount)
		w.PATCH("/chart-of-accounts/:id", api.UpdateChartAccount)
		w.DELETE("/chart-of-accounts/:id", api.DeleteChartAccount)
		v1.GET("/ledger/entries", ledgerRead, api.ListJournalEntries)
		v1.GET("/ledger/entries/:id", ledgerRead, api.GetJournalEntry)
		w.POST("/ledger/entries", api.CreateJournalEntry)
		w.POST("/ledger/entries/:id/post", api.PostJournalEntry)
		w.POST("/ledger/entries/:id/reverse", api.ReverseJournalEntry)
		w.POST("/ledger/validate-posting", ops.ValidatePosting)
		v1.GET("/ledger/periods", ledgerRead, api.ListFiscalPeriods)
		w.POST("/ledger/periods/:period/close", api.CloseFiscalPeriod)
		w.POST("/ledger/periods/:period/reopen", api.ReopenFiscalPeriod)
		w.POST("/ledger/year-end/:year/close", api.CloseFiscalYear)
		v1.GET("/fixed-assets", ledgerRead, api.ListFixedAssets)
		w.POST("/fixed-assets", api.RegisterFixedAsset)
		// IAS 38 intangible assets subledger (mirrors fixed-assets).
		v1.GET("/intangible-assets", ledgerRead, api.ListIntangibleAssets)
		w.POST("/intangible-assets", api.RegisterIntangibleAsset)
		w.POST("/fixed-assets/depreciation/run", api.RunDepreciation)
		// IAS 16 / IAS 36 — impairment and revaluation.
		w.POST("/fixed-assets/impair", api.ImpairAsset)
		w.POST("/fixed-assets/reverse-impairment", api.ReverseImpairmentAsset)
		w.POST("/fixed-assets/revalue", api.RevalueAsset)
		v1.GET("/approvals", ledgerRead, api.ListApprovals)
		v1.GET("/approvals/tiers", ledgerRead, api.ListApprovalTiers)
		w.POST("/approvals", api.SubmitApproval)
		w.POST("/approvals/:id/approve", api.ApproveApproval)
		w.POST("/approvals/:id/reject", api.RejectApproval)
		v1.GET("/reports/trial-balance", ledgerRead, api.TrialBalance)
		v1.GET("/reports/ar-aging", ledgerRead, api.ARAging)
		v1.GET("/reports/ap-aging", ledgerRead, api.APAging)
		v1.GET("/reports/profit-and-loss", ledgerRead, api.ProfitAndLoss)
		v1.GET("/reports/balance-sheet", ledgerRead, api.BalanceSheet)
		v1.GET("/reports/gl-account/:code", ledgerRead, api.GLAccountDetail)
		v1.GET("/finance/summary", ledgerRead, api.FinanceSummary)
		v1.GET("/fx/rates", ledgerRead, api.ListExchangeRates)
		w.POST("/fx/rates", api.UpsertExchangeRate)
		w.POST("/fx/revalue", api.RevalueFX)
		// FX conversions (treasury; record-only — see migration 061).
		v1.GET("/fx/conversions", ledgerRead, api.ListFXConversions)
		w.POST("/fx/conversions", api.RecordFXConversion)
		v1.GET("/tax-codes", ledgerRead, api.ListTaxCodes)
		w.POST("/tax-codes", api.UpsertTaxCode)
		w.POST("/tax/reverse-charge", api.SelfAssessReverseCharge)
		// Withholding-tax receipts (WHT recoverable subledger).
		v1.GET("/tax/withholding", ledgerRead, api.ListWHTReceipts)
		w.POST("/tax/withholding", api.RecordWHTReceipt)
		// Late fees (Dr AR / Cr late-fee income).
		v1.GET("/ar/late-fees", ledgerRead, api.ListLateFees)
		w.POST("/ar/late-fees", api.RecordLateFee)
		// Billable time entries (unbilled; no GL until invoiced).
		v1.GET("/billing/time-entries", ledgerRead, api.ListTimeEntries)
		w.POST("/billing/time-entries", api.RecordTimeEntry)
		v1.GET("/reports/vat-return", ledgerRead, api.VATReturn)
		v1.GET("/entities", ledgerRead, api.ListEntities)
		w.POST("/entities", api.CreateEntity)
		w.PATCH("/entities/:id/ownership", api.SetEntityOwnership)
		// IFRS 10 — consolidation eliminations (intra-group + investment/equity).
		v1.GET("/consolidation/eliminations", ledgerRead, api.ConsolidationEliminations)
		w.POST("/budgets", api.UpsertBudget)
		v1.GET("/reports/budget-vs-actual", ledgerRead, api.BudgetVsActual)
		v1.GET("/reports/cash-flow", ledgerRead, api.CashFlow)
		v1.GET("/reports/sales-by-item", ledgerRead, api.SalesByItem)
		v1.GET("/reports/changes-in-equity", ledgerRead, api.ChangesInEquity)
		v1.GET("/reports/control-reconciliation", ledgerRead, api.ControlReconciliation)
		v1.GET("/billing/invoices", ledgerRead, api.ListBillingInvoices)
		w.POST("/billing/invoices", api.CreateInvoice)
		v1.GET("/billing/invoices/:id", ledgerRead, api.GetBillingInvoice)
		w.POST("/billing/invoices/:id/issue", api.IssueInvoice)
		v1.GET("/billing/recurring", ledgerRead, api.ListRecurringInvoices)
		w.POST("/billing/recurring", api.CreateRecurringInvoice)
		w.POST("/payments/intents", api.CreatePaymentIntent)
		w.POST("/payments/intents/:id/confirm", api.ConfirmPaymentIntent)
		v1.GET("/projects", ledgerRead, api.ListProjects)
		w.POST("/projects", api.CreateProject)
		v1.GET("/projects/:id/profit-and-loss", ledgerRead, api.ProjectPL)
		v1.GET("/cost-centers", ledgerRead, api.ListCostCenters)
		w.POST("/cost-centers", api.CreateCostCenter)
		// Customer / vendor billing-party master — backs the frontend
		// supplier/customer dropdowns and inline create-new.
		v1.GET("/customers", ledgerRead, api.ListCustomers)
		w.POST("/customers", api.CreateCustomer)
		v1.GET("/vendors", ledgerRead, api.ListVendors)
		w.POST("/vendors", api.CreateVendor)
		v1.GET("/invoices", ledgerRead, api.ListInvoicesLegacy)
		w.POST("/invoices", api.CreateInvoiceLegacy)
		v1.GET("/invoices/funnel", ledgerRead, api.InvoiceFunnel)
		v1.GET("/invoices/:no", ledgerRead, api.GetInvoiceLegacy)
		w.PATCH("/invoices/:no", api.PatchInvoiceLegacy)
		w.DELETE("/invoices/:no", api.DeleteInvoiceLegacy)
		// Legacy AP "bills" CRUD — the AP counterpart of /invoices (→ ap_open_items)
		v1.GET("/bills", ledgerRead, api.ListBillsLegacy)
		w.POST("/bills", api.CreateBillLegacy)
		v1.GET("/bills/:no", ledgerRead, api.GetBillLegacy)
		w.PATCH("/bills/:no", api.PatchBillLegacy)
		w.DELETE("/bills/:no", api.DeleteBillLegacy)
		v1.GET("/banking/accounts", ledgerRead, api.ListBankingAccounts)
		v1.GET("/banking/transactions", ledgerRead, api.ListBankingTransactions)
		// Bank reference list (licensed banks + mobile money + petty cash) backing
		// the frontend "Bank Name" dropdown when creating a bank account.
		v1.GET("/banks", ledgerRead, api.ListBanks)

		// AR / AP — POST publishes sale.completed / invoice.posted for async GL booking
		v1.GET("/ar/items", ledgerRead, api.ListARItems)
		w.POST("/ar/items", api.CreateARItem)
		w.POST("/ar/items/:id/payments", api.ApplyARPayment)
		v1.GET("/ar/items/:id/payments", ledgerRead, api.ListARPayments)
		v1.GET("/ar/items/:id/payment-link", ledgerRead, api.PaymentLink)
		v1.GET("/ar/invoices/:documentRef/pdf", ledgerRead, api.InvoicePDF)
		w.POST("/ar/invoices/:documentRef/email", api.EmailInvoice)
		v1.GET("/ar/customers/:customerRef/statement", ledgerRead, api.CustomerStatement)
		w.POST("/ar/credit-notes", api.CreateARCreditNote)
		w.POST("/ar/debit-notes", api.CreateARDebitNote)
		w.POST("/ap/credit-notes", api.CreateAPCreditNote)
		w.POST("/ap/debit-notes", api.CreateAPDebitNote)
		v1.GET("/adjustments", ledgerRead, api.ListAdjustments)
		// IFRS 9 — expected-credit-loss provisioning, write-off and recovery.
		v1.GET("/provisions/ecl", ledgerRead, api.ListECLProvisions)
		w.POST("/provisions/ecl-run", api.RunECLProvision)
		w.POST("/provisions/write-off", api.WriteOffReceivable)
		w.POST("/provisions/recover", api.RecoverReceivable)
		// IFRS 15 — deferred/accrued revenue and scheduled recognition.
		v1.GET("/revenue/schedules", ledgerRead, api.ListRevenueSchedules)
		w.POST("/revenue/schedules", api.CreateRevenueSchedule)
		w.POST("/revenue/recognition-run", api.RunRevenueRecognition)
		w.POST("/revenue/obligations/:id/satisfy", api.SatisfyObligation)
		w.POST("/revenue/accrue", api.AccrueRevenue)
		// IAS 1 matching — prepaid-expense amortization (expense-side mirror of IFRS 15).
		v1.GET("/prepayments", ledgerRead, api.ListPrepayments)
		w.POST("/prepayments", api.CreatePrepayment)
		w.POST("/prepayments/amortization-run", api.RunAmortization)
		// IFRS 16 — leases (right-of-use asset + lease liability).
		v1.GET("/leases", ledgerRead, api.ListLeases)
		w.POST("/leases", api.CreateLease)
		w.POST("/leases/run", api.RunLeasePeriod)
		// IAS 12 — income taxes (current provision + deferred tax).
		v1.GET("/income-tax/runs", ledgerRead, api.ListIncomeTaxRuns)
		w.POST("/income-tax/current-run", api.RunCurrentTax)
		v1.GET("/income-tax/deferred", ledgerRead, api.ListDeferredTaxItems)
		w.POST("/income-tax/deferred", api.RecognizeDeferredTax)
		// IAS 37 — provisions & decommissioning.
		v1.GET("/provisions/liability", ledgerRead, api.ListProvisions)
		w.POST("/provisions/liability/recognize", api.RecognizeProvision)
		w.POST("/provisions/liability/unwind", api.UnwindProvision)
		w.POST("/provisions/liability/utilize", api.UtilizeProvision)
		w.POST("/provisions/liability/remeasure", api.RemeasureProvision)
		w.POST("/provisions/liability/reverse", api.ReverseProvision)
		// Three-way match — GR/IR variance & orphan detection.
		v1.GET("/procurement/match-exceptions", ledgerRead, api.ListMatchExceptions)
		w.POST("/procurement/match-check", api.RunMatchCheck)
		w.POST("/procurement/match-exceptions/:id/resolve", api.ResolveMatchException)
		w.POST("/procurement/match-variance/write-off", api.WriteOffMatchVariance)
		v1.GET("/ap/items", ledgerRead, api.ListAPItems)
		w.POST("/ap/items", api.CreateAPItem)
		w.POST("/ap/items/:id/payments", api.ApplyAPPayment)
		v1.GET("/ap/items/:id/payments", ledgerRead, api.ListAPPayments)
		v1.GET("/inbox/bank-accounts", opsRead, api.ListBankAccounts)
		v1.GET("/inbox/ap", opsRead, api.ListAPInbox)
		v1.GET("/inbox/cherry-intake", opsRead, api.ListCherryIntake)

		v1.GET("/payroll/employees", viewPayroll, api.ListPayrollEmployees)
		v1.GET("/payroll/leave-accruals", viewPayroll, api.ListPayrollLeaveAccruals)
		v1.GET("/payroll/runs", ledgerRead, api.ListPayrollRuns)
		w.POST("/payroll/runs", api.PostPayrollRun)

		v1.GET("/portal/me", middleware.RequirePortalAP(), api.PortalMe)
		v1.GET("/portal/ap", middleware.RequirePortalAP(), api.PortalAP)

		// Hash-chain ops audit (prototype UI)
		v1.GET("/audit/events", opsRead, ops.ListAudit)
		v1.GET("/audit/events/verify", opsRead, ops.VerifyAudit)
		w.POST("/audit/events", ops.AppendAudit)
		v1.GET("/tables/:tableId/rows", opsRead, ops.ListTableRows)
		w.POST("/tables/:tableId/rows", ops.AppendTableRow)

		admin := v1.Group("/admin", middleware.RequireAdmin())
		admin.GET("/audit", api.ListAuditLogs)
		admin.GET("/audit/:id", api.GetAuditLog)
		admin.GET("/monitoring/summary", api.MonitoringSummary)
		admin.GET("/monitoring/activity", api.MonitoringActivity)
		admin.GET("/monitoring/ledger", api.MonitoringLedger)
	}

	return router
}

// NewLedger builds ledger + audit services from the pool, configured with the
// base/reporting currency used for FX conversion.
func NewLedger(pool *pgxpool.Pool, baseCurrency string) (*ledger.Service, *auditlog.Service) {
	repo := repository.New(pool)
	repo.SetBaseCurrency(baseCurrency)
	return ledger.New(repo), auditlog.New(repo)
}

// newConfiguredRepo builds a repository with the base currency applied.
func newConfiguredRepo(deps RouterDeps) *repository.Repository {
	repo := repository.New(deps.Pool)
	repo.SetBaseCurrency(deps.Config.BaseCurrency)
	return repo
}
