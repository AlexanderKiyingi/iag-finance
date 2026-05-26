package router

import (
	"github.com/gin-gonic/gin"
	"github.com/iag/finance-backend/internal/app"
	"github.com/iag/finance-backend/internal/config"
	"github.com/iag/finance-backend/internal/middleware"
)

func New(cfg config.Config) (*gin.Engine, func()) {
	if cfg.IsProduction {
		gin.SetMode(gin.ReleaseMode)
	}

	mvc, cleanup := app.NewMVC(cfg)

	r := gin.New()
	r.Use(middleware.GinRecovery())
	r.Use(middleware.GinLogger())
	r.Use(middleware.GinRateLimit(cfg.RateLimitPerMin))
	r.Use(middleware.GinCORSDev(cfg))

	if cfg.JWTRequired && mvc.JWT != nil {
		r.Use(middleware.GinJWTAuth(cfg, mvc.JWT, mvc.Store))
	}

	for _, base := range []string{"", "/v1"} {
		registerRoutes(r.Group(base), mvc, cfg)
	}

	return r, cleanup
}

func registerRoutes(g *gin.RouterGroup, mvc *app.MVC, cfg config.Config) {
	wrap := gin.WrapF

	g.GET("/health", wrap(mvc.Health.Check))
	g.GET("/ready", wrap(mvc.Health.Ready))

	g.GET("/bootstrap", wrap(mvc.Bootstrap.Bootstrap))
	g.GET("/auth/accounts", wrap(mvc.Bootstrap.Accounts))
	g.GET("/auth/session", wrap(mvc.Bootstrap.Session))
	g.POST("/auth/login", wrap(mvc.Bootstrap.Login))
	g.POST("/auth/refresh", wrap(mvc.Bootstrap.Refresh))
	g.POST("/auth/logout", wrap(mvc.Bootstrap.Logout))
	g.PATCH("/auth/session", wrap(mvc.Bootstrap.PatchSession))
	if cfg.AllowDemoReset {
		g.POST("/demo/reset", wrap(mvc.Bootstrap.ResetDemo))
	}

	g.GET("/dashboard", wrap(mvc.Dashboard.Get))
	g.GET("/updates", wrap(mvc.Dashboard.Updates))

	g.GET("/invoices", wrap(mvc.Invoices.List))
	g.POST("/invoices", wrap(mvc.Invoices.Create))
	g.GET("/invoices/funnel", wrap(mvc.Invoices.Funnel))
	g.GET("/invoices/:no", wrap(mvc.Invoices.Get))
	g.PATCH("/invoices/:no", wrap(mvc.Invoices.Patch))
	g.PUT("/invoices/:no", wrap(mvc.Invoices.Patch))
	g.DELETE("/invoices/:no", wrap(mvc.Invoices.Delete))

	g.GET("/banking/accounts", wrap(mvc.Banking.ListAccounts))
	g.GET("/banking/transactions", wrap(mvc.Banking.ListTransactions))

	g.GET("/assets", wrap(mvc.Assets.List))
	g.GET("/assets/:tag", wrap(mvc.Assets.Get))

	g.GET("/approvals", wrap(mvc.Approvals.List))
	g.GET("/approvals/:id", wrap(mvc.Approvals.Get))
	g.PATCH("/approvals/:id", wrap(mvc.Approvals.Patch))

	g.GET("/audit", wrap(mvc.Audit.List))
	g.POST("/audit", wrap(mvc.Audit.Create))

	g.GET("/expenses", wrap(mvc.Modules.Expenses))
	g.GET("/workers", wrap(mvc.Modules.Workers))
	g.GET("/users", wrap(mvc.Modules.Users))
	g.GET("/notifications", wrap(mvc.Modules.Notifications))
	g.GET("/budgets", wrap(mvc.Modules.Budgets))
	g.GET("/journals", wrap(mvc.Modules.Journals))
	g.GET("/inventory", wrap(mvc.Modules.Inventory))
	g.GET("/taxes", wrap(mvc.Modules.Taxes))

	g.GET("/settings", wrap(mvc.Settings.Get))
	g.PATCH("/settings", wrap(mvc.Settings.Patch))
}
