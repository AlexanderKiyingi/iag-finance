package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/views"
)

type BankingController struct {
	store *models.Store
}

func NewBankingController(store *models.Store) *BankingController {
	return &BankingController{store: store}
}

func (c *BankingController) ListAccounts(w http.ResponseWriter, r *http.Request) {
	items := c.store.ListBankAccounts()
	views.JSON(w, http.StatusOK, map[string]any{
		"items": items,
		"meta":  models.ListMeta{Total: len(items)},
	})
}

func (c *BankingController) ListTransactions(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListBankTx(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}
