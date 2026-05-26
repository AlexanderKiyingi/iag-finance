package app

import (
	"github.com/iag/finance-backend/internal/auth"
	"github.com/iag/finance-backend/internal/config"
	"github.com/iag/finance-backend/internal/controllers"
	"github.com/iag/finance-backend/internal/models"
)

type MVC struct {
	Store      *models.Store
	Cfg        config.Config
	JWT        *auth.Service
	Health     *controllers.HealthController
	Bootstrap  *controllers.BootstrapController
	Dashboard  *controllers.DashboardController
	Invoices   *controllers.InvoiceController
	Banking    *controllers.BankingController
	Assets     *controllers.AssetController
	Approvals  *controllers.ApprovalController
	Audit      *controllers.AuditController
	Modules    *controllers.ModulesController
	Settings   *controllers.SettingsController
}

func NewMVC(cfg config.Config) (*MVC, func()) {
	store, jwtSvc, cleanup := buildStore(cfg)
	return &MVC{
		Store:      store,
		Cfg:        cfg,
		JWT:        jwtSvc,
		Health:     controllers.NewHealthController(),
		Bootstrap:  controllers.NewBootstrapController(store, cfg),
		Dashboard:  controllers.NewDashboardController(store),
		Invoices:   controllers.NewInvoiceController(store),
		Banking:    controllers.NewBankingController(store),
		Assets:     controllers.NewAssetController(store),
		Approvals:  controllers.NewApprovalController(store),
		Audit:      controllers.NewAuditController(store),
		Modules:    controllers.NewModulesController(store),
		Settings:   controllers.NewSettingsController(store),
	}, cleanup
}
