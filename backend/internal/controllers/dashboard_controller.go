package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/views"
)

type DashboardController struct {
	store *models.Store
}

func NewDashboardController(store *models.Store) *DashboardController {
	return &DashboardController{store: store}
}

func (c *DashboardController) Get(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, c.store.Dashboard())
}

func (c *DashboardController) Updates(w http.ResponseWriter, r *http.Request) {
	d := c.store.Dashboard()
	views.JSON(w, http.StatusOK, map[string]any{"items": d.Updates})
}
