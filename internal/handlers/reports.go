package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) ARAging(c *gin.Context) {
	rows, err := a.Ledger.ARAging(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build AR aging")
		return
	}
	c.JSON(http.StatusOK, gin.H{"buckets": rows})
}

func (a *API) ProfitAndLoss(c *gin.Context) {
	rows, err := a.Ledger.ProfitAndLoss(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build P&L")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

func (a *API) BalanceSheet(c *gin.Context) {
	rows, err := a.Ledger.BalanceSheet(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build balance sheet")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

func (a *API) FinanceSummary(c *gin.Context) {
	summary, err := a.Ledger.FinanceSummary(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build summary")
		return
	}
	c.JSON(http.StatusOK, summary)
}
