package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// dateParam parses a 'YYYY-MM-DD' query param into a *time.Time (nil if absent
// or malformed — unbounded on that side).
func dateParam(c *gin.Context, name string) *time.Time {
	raw := c.Query(name)
	if raw == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil
	}
	return &t
}

func (a *API) ARAging(c *gin.Context) {
	rows, err := a.Ledger.ARAging(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build AR aging")
		return
	}
	c.JSON(http.StatusOK, gin.H{"buckets": rows})
}

func (a *API) APAging(c *gin.Context) {
	rows, err := a.Ledger.APAging(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build AP aging")
		return
	}
	c.JSON(http.StatusOK, gin.H{"buckets": rows})
}

// GLAccountDetail returns one account's posted postings with a running balance,
// bounded by optional ?from=/?to= dates. The account code is the path param.
func (a *API) GLAccountDetail(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		apierr.JSONStatus(c, http.StatusBadRequest, "account code is required")
		return
	}
	rows, err := a.Ledger.GLAccountDetail(c.Request.Context(), code, dateParam(c, "from"), dateParam(c, "to"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build account detail")
		return
	}
	c.JSON(http.StatusOK, gin.H{"accountCode": code, "lines": rows})
}

func (a *API) ProfitAndLoss(c *gin.Context) {
	rows, err := a.Ledger.ProfitAndLoss(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build P&L")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

func (a *API) BalanceSheet(c *gin.Context) {
	// Balance sheet is point-in-time: honour ?to=/?asOf= as the upper bound.
	asOf := dateParam(c, "asOf")
	if asOf == nil {
		asOf = dateParam(c, "to")
	}
	rows, err := a.Ledger.BalanceSheet(c.Request.Context(), asOf)
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
