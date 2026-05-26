package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/views"
)

type ModulesController struct {
	store *models.Store
}

func NewModulesController(store *models.Store) *ModulesController {
	return &ModulesController{store: store}
}

func listResponse[T any](items []T, p query.Page) map[string]any {
	return map[string]any{
		"items": items,
		"meta":  models.ListMeta{Total: p.Total, Page: p.Page, Limit: p.Limit},
	}
}

func (c *ModulesController) Expenses(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListExpenses(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Workers(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListWorkers(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Users(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListUsers(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Notifications(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListNotifications(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Budgets(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListBudgets(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Journals(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListJournals(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Inventory(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListInventory(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ModulesController) Taxes(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListTaxes(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}
