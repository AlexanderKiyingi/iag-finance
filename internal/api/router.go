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
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/middleware"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/iag-finance/backend/internal/tablerows"
	"github.com/iag-finance/backend/internal/tenant"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type RouterDeps struct {
	Config   config.Config
	Pool     *pgxpool.Pool
	Redis    *redis.Client
	Verifier *authclient.Verifier
	Ledger   *ledger.Service
	AuditLog *auditlog.Service
	Events   *events.Bus
}

func NewRouter(deps RouterDeps) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), middleware.SecurityHeaders())

	corsCfg := cors.Config{
		AllowOrigins:     deps.Config.CORSAllowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", tenant.Header},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * 3600,
	}
	if len(corsCfg.AllowOrigins) == 0 {
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

	api := &handlers.API{
		Ledger:          deps.Ledger,
		Audit:           deps.AuditLog,
		DB:              &db.PoolHealth{Pool: deps.Pool},
		ConsumerEnabled: deps.Config.EnableConsumer,
		Events:          deps.Events,
	}

	router.GET("/health", ops.Health)
	router.GET("/ready", ops.Ready)

	principal := middleware.Principal(deps.Verifier)
	ledgerRead := middleware.RequireLedgerRead()
	ledgerWrite := middleware.RequireLedgerWrite()
	opsRead := middleware.RequireOperationsRead()
	opsWrite := middleware.RequireOperationsWrite()

	v1 := router.Group("/v1", principal, middleware.AuditLog(deps.AuditLog))
	{
		// Integrations
		v1.GET("/integrations/ura-efris", ledgerRead, api.URAStatus)
		v1.GET("/integrations/banking", ledgerRead, api.BankingStatus)

		// General ledger (accounting)
		v1.GET("/chart-of-accounts", ledgerRead, api.ListChartOfAccounts)
		v1.POST("/chart-of-accounts", ledgerWrite, api.CreateChartAccount)
		v1.GET("/ledger/entries", ledgerRead, api.ListJournalEntries)
		v1.GET("/ledger/entries/:id", ledgerRead, api.GetJournalEntry)
		v1.POST("/ledger/entries", ledgerWrite, api.CreateJournalEntry)
		v1.POST("/ledger/entries/:id/post", ledgerWrite, api.PostJournalEntry)
		v1.POST("/ledger/validate-posting", ledgerWrite, ops.ValidatePosting)
		v1.GET("/reports/trial-balance", ledgerRead, api.TrialBalance)

		// AR / AP — POST publishes sale.completed / invoice.posted for async GL booking
		v1.GET("/ar/items", ledgerRead, api.ListARItems)
		v1.POST("/ar/items", ledgerWrite, api.CreateARItem)
		v1.GET("/ap/items", ledgerRead, api.ListAPItems)
		v1.POST("/ap/items", ledgerWrite, api.CreateAPItem)
		v1.GET("/inbox/bank-accounts", opsRead, api.ListBankAccounts)
		v1.GET("/inbox/ap", opsRead, api.ListAPInbox)
		v1.GET("/inbox/cherry-intake", opsRead, api.ListCherryIntake)

		v1.GET("/portal/me", middleware.RequirePortalAP(), api.PortalMe)
		v1.GET("/portal/ap", middleware.RequirePortalAP(), api.PortalAP)

		// Hash-chain ops audit (prototype UI)
		v1.GET("/audit/events", opsRead, ops.ListAudit)
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

// NewLedger builds ledger + audit services from the pool.
func NewLedger(pool *pgxpool.Pool) (*ledger.Service, *auditlog.Service) {
	repo := repository.New(pool)
	return ledger.New(repo), auditlog.New(repo)
}
