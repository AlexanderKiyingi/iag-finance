package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/tenant"
)

func (a *API) ListBankAccounts(c *gin.Context) {
	items, err := a.Ledger.ListBankAccounts(c.Request.Context(), tenant.FromGin(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list bank accounts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListAPInbox(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListAPItems(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list AP inbox"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "ap_open_items"})
}

func (a *API) ListCherryIntake(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := a.Ledger.ListCherryIntake(c.Request.Context(), tenant.FromGin(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list cherry intake"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
