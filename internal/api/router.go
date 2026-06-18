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
	ledgerWrite := middleware.RequireLedgerWrite()
	opsRead := middleware.RequireOperationsRead()
	opsWrite := middleware.RequireOperationsWrite()

	// Granular capability gates (separation of duties). Each accepts its specific
	// permission OR the broad finance.change_ledger, so existing change_ledger
	// grants keep working; drop change_ledger from a role to enforce the narrow
	// grant. Superuser bypasses all of these.
	const cw = "finance.change_ledger"
	manageCOA := middleware.Require("finance.manage_coa", cw)
	reverseJournal := middleware.Require("finance.reverse_journal", cw)
	closePeriod := middleware.Require("finance.close_period", cw)
	runDepreciation := middleware.Require("finance.run_depreciation", cw)
	manageFX := middleware.Require("finance.manage_fx", cw)
	manageTax := middleware.Require("finance.manage_tax", cw)
	submitEFRIS := middleware.Require("finance.submit_efris", cw)
	manageEntities := middleware.Require("finance.manage_entities", cw)
	manageBudgets := middleware.Require("finance.manage_budgets", cw)
	manageDimensions := middleware.Require("finance.manage_dimensions", cw)
	issueInvoice := middleware.Require("finance.issue_invoice", cw)
	collectPayment := middleware.Require("finance.collect_payment", cw)
	viewPayroll := middleware.Require("finance.view_payroll", "finance.view_ledger", "finance.view_operations")

	v1 := router.Group("/v1", principal, middleware.EntityContext(), middleware.AuditLog(deps.AuditLog))
	{
		// Integrations
		v1.GET("/integrations/ura-efris", ledgerRead, api.URAStatus)
		v1.POST("/integrations/ura-efris/submit", submitEFRIS, api.SubmitEFRIS)
		v1.GET("/integrations/banking", ledgerRead, api.BankingStatus)
		v1.POST("/integrations/banking/statements", ledgerWrite, api.ImportBankStatement)
		v1.GET("/integrations/banking/statements/:id/lines", ledgerRead, api.ListStatementLines)
		v1.POST("/integrations/banking/statements/:id/reconcile/auto", ledgerWrite, api.AutoReconcileStatement)
		v1.POST("/integrations/banking/lines/:lineId/match", ledgerWrite, api.MatchStatementLine)
		v1.POST("/integrations/banking/lines/:lineId/confirm", ledgerWrite, api.ConfirmStatementLine)
		v1.POST("/integrations/banking/lines/:lineId/reject", ledgerWrite, api.RejectStatementLine)
		v1.POST("/integrations/banking/sync", ledgerWrite, api.SyncBankFeed)

		// General ledger (accounting)
		v1.GET("/chart-of-accounts", ledgerRead, api.ListChartOfAccounts)
		v1.POST("/chart-of-accounts", manageCOA, api.CreateChartAccount)
		v1.GET("/ledger/entries", ledgerRead, api.ListJournalEntries)
		v1.GET("/ledger/entries/:id", ledgerRead, api.GetJournalEntry)
		v1.POST("/ledger/entries", ledgerWrite, api.CreateJournalEntry)
		v1.POST("/ledger/entries/:id/post", ledgerWrite, api.PostJournalEntry)
		v1.POST("/ledger/entries/:id/reverse", reverseJournal, api.ReverseJournalEntry)
		v1.POST("/ledger/validate-posting", ledgerWrite, ops.ValidatePosting)
		v1.GET("/ledger/periods", ledgerRead, api.ListFiscalPeriods)
		v1.POST("/ledger/periods/:period/close", closePeriod, api.CloseFiscalPeriod)
		v1.POST("/ledger/periods/:period/reopen", closePeriod, api.ReopenFiscalPeriod)
		v1.POST("/ledger/year-end/:year/close", closePeriod, api.CloseFiscalYear)
		v1.GET("/fixed-assets", ledgerRead, api.ListFixedAssets)
		v1.POST("/fixed-assets", runDepreciation, api.RegisterFixedAsset)
		v1.POST("/fixed-assets/depreciation/run", runDepreciation, api.RunDepreciation)
		v1.GET("/approvals", ledgerRead, api.ListApprovals)
		v1.GET("/approvals/tiers", ledgerRead, api.ListApprovalTiers)
		v1.POST("/approvals", ledgerWrite, api.SubmitApproval)
		v1.POST("/approvals/:id/approve", ledgerWrite, api.ApproveApproval)
		v1.POST("/approvals/:id/reject", ledgerWrite, api.RejectApproval)
		v1.GET("/reports/trial-balance", ledgerRead, api.TrialBalance)
		v1.GET("/reports/ar-aging", ledgerRead, api.ARAging)
		v1.GET("/reports/ap-aging", ledgerRead, api.APAging)
		v1.GET("/reports/profit-and-loss", ledgerRead, api.ProfitAndLoss)
		v1.GET("/reports/balance-sheet", ledgerRead, api.BalanceSheet)
		v1.GET("/reports/gl-account/:code", ledgerRead, api.GLAccountDetail)
		v1.GET("/finance/summary", ledgerRead, api.FinanceSummary)
		v1.GET("/fx/rates", ledgerRead, api.ListExchangeRates)
		v1.POST("/fx/rates", manageFX, api.UpsertExchangeRate)
		v1.POST("/fx/revalue", manageFX, api.RevalueFX)
		v1.GET("/tax-codes", ledgerRead, api.ListTaxCodes)
		v1.POST("/tax-codes", manageTax, api.UpsertTaxCode)
		v1.GET("/reports/vat-return", ledgerRead, api.VATReturn)
		v1.GET("/entities", ledgerRead, api.ListEntities)
		v1.POST("/entities", manageEntities, api.CreateEntity)
		v1.POST("/budgets", manageBudgets, api.UpsertBudget)
		v1.GET("/reports/budget-vs-actual", ledgerRead, api.BudgetVsActual)
		v1.GET("/reports/cash-flow", ledgerRead, api.CashFlow)
		v1.GET("/billing/invoices", ledgerRead, api.ListBillingInvoices)
		v1.POST("/billing/invoices", issueInvoice, api.CreateInvoice)
		v1.GET("/billing/invoices/:id", ledgerRead, api.GetBillingInvoice)
		v1.POST("/billing/invoices/:id/issue", issueInvoice, api.IssueInvoice)
		v1.GET("/billing/recurring", ledgerRead, api.ListRecurringInvoices)
		v1.POST("/billing/recurring", issueInvoice, api.CreateRecurringInvoice)
		v1.POST("/payments/intents", collectPayment, api.CreatePaymentIntent)
		v1.POST("/payments/intents/:id/confirm", collectPayment, api.ConfirmPaymentIntent)
		v1.GET("/projects", ledgerRead, api.ListProjects)
		v1.POST("/projects", manageDimensions, api.CreateProject)
		v1.GET("/projects/:id/profit-and-loss", ledgerRead, api.ProjectPL)
		v1.GET("/cost-centers", ledgerRead, api.ListCostCenters)
		v1.POST("/cost-centers", manageDimensions, api.CreateCostCenter)
		v1.GET("/invoices", ledgerRead, api.ListInvoicesLegacy)
		v1.POST("/invoices", ledgerWrite, api.CreateInvoiceLegacy)
		v1.GET("/invoices/funnel", ledgerRead, api.InvoiceFunnel)
		v1.GET("/invoices/:no", ledgerRead, api.GetInvoiceLegacy)
		v1.PATCH("/invoices/:no", ledgerWrite, api.PatchInvoiceLegacy)
		v1.DELETE("/invoices/:no", ledgerWrite, api.DeleteInvoiceLegacy)
		v1.GET("/banking/accounts", ledgerRead, api.ListBankingAccounts)
		v1.GET("/banking/transactions", ledgerRead, api.ListBankingTransactions)

		// AR / AP — POST publishes sale.completed / invoice.posted for async GL booking
		v1.GET("/ar/items", ledgerRead, api.ListARItems)
		v1.POST("/ar/items", ledgerWrite, api.CreateARItem)
		v1.POST("/ar/items/:id/payments", ledgerWrite, api.ApplyARPayment)
		v1.GET("/ar/items/:id/payments", ledgerRead, api.ListARPayments)
		v1.GET("/ar/items/:id/payment-link", ledgerRead, api.PaymentLink)
		v1.GET("/ar/invoices/:documentRef/pdf", ledgerRead, api.InvoicePDF)
		v1.GET("/ar/customers/:customerRef/statement", ledgerRead, api.CustomerStatement)
		v1.POST("/ar/credit-notes", ledgerWrite, api.CreateARCreditNote)
		v1.POST("/ar/debit-notes", ledgerWrite, api.CreateARDebitNote)
		v1.POST("/ap/credit-notes", ledgerWrite, api.CreateAPCreditNote)
		v1.POST("/ap/debit-notes", ledgerWrite, api.CreateAPDebitNote)
		v1.GET("/adjustments", ledgerRead, api.ListAdjustments)
		v1.GET("/ap/items", ledgerRead, api.ListAPItems)
		v1.POST("/ap/items", ledgerWrite, api.CreateAPItem)
		v1.POST("/ap/items/:id/payments", ledgerWrite, api.ApplyAPPayment)
		v1.GET("/ap/items/:id/payments", ledgerRead, api.ListAPPayments)
		v1.GET("/inbox/bank-accounts", opsRead, api.ListBankAccounts)
		v1.GET("/inbox/ap", opsRead, api.ListAPInbox)
		v1.GET("/inbox/cherry-intake", opsRead, api.ListCherryIntake)

		v1.GET("/payroll/employees", viewPayroll, api.ListPayrollEmployees)
		v1.GET("/payroll/leave-accruals", viewPayroll, api.ListPayrollLeaveAccruals)
		v1.GET("/payroll/runs", ledgerRead, api.ListPayrollRuns)
		v1.POST("/payroll/runs", ledgerWrite, api.PostPayrollRun)

		v1.GET("/portal/me", middleware.RequirePortalAP(), api.PortalMe)
		v1.GET("/portal/ap", middleware.RequirePortalAP(), api.PortalAP)

		// Hash-chain ops audit (prototype UI)
		v1.GET("/audit/events", opsRead, ops.ListAudit)
		v1.GET("/audit/events/verify", opsRead, ops.VerifyAudit)
		v1.POST("/audit/events", opsWrite, ops.AppendAudit)
		v1.GET("/tables/:tableId/rows", opsRead, ops.ListTableRows)
		v1.POST("/tables/:tableId/rows", opsWrite, ops.AppendTableRow)

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
