package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/middleware"
)

// entityScope resolves the entity ids a report should cover from the request:
// the current entity (X-Entity-Id header, else default), or it plus descendants
// when ?consolidated=true. Consolidation requires finance.view_consolidated;
// without it the flag is ignored (single-entity scope) rather than exposing
// cross-entity data.
func (a *API) entityScope(c *gin.Context) ([]uuid.UUID, error) {
	consolidated := c.Query("consolidated") == "true" && middleware.HasPerm(c, "finance.view_consolidated")
	return a.Ledger.EntityScope(c.Request.Context(), consolidated)
}

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
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	rows, err := a.Ledger.ProfitAndLoss(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
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
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	rows, err := a.Ledger.BalanceSheet(c.Request.Context(), asOf, scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build balance sheet")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

type upsertBudgetRequest struct {
	Period      string `json:"period" binding:"required"` // YYYY-MM
	AccountCode string `json:"accountCode" binding:"required"`
	Amount      string `json:"amount" binding:"required"`
}

// UpsertBudget sets an account's budget for a period (entity from X-Entity-Id).
func (a *API) UpsertBudget(c *gin.Context) {
	var req upsertBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Ledger.UpsertBudget(c.Request.Context(), req.Period, req.AccountCode, req.Amount); err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not save budget")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "saved"})
}

// BudgetVsActual reports budget vs actual per account over ?from=&to=.
func (a *API) BudgetVsActual(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	rows, err := a.Ledger.BudgetVsActual(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build budget vs actual")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

// ControlReconciliation ties each subledger control account (AR 1100, AP 2000)
// to the sum of its open items so a GL↔subledger drift is surfaced.
func (a *API) ControlReconciliation(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	rows, err := a.Ledger.ControlReconciliation(c.Request.Context(), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build control reconciliation")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

// CashFlow reports cash movement by activity category over ?from=&to=.
func (a *API) SalesByItem(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := a.Ledger.SalesByItem(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build sales-by-item report")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

func (a *API) ChangesInEquity(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := a.Ledger.StatementOfChangesInEquity(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build statement of changes in equity")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

func (a *API) CashFlow(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	// ?method=indirect returns the IAS 7 indirect (net-income reconciliation)
	// statement; the default remains the direct activity split.
	if c.Query("method") == "indirect" {
		report, err := a.Ledger.IndirectCashFlow(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
		if err != nil {
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not build cash flow")
			return
		}
		c.JSON(http.StatusOK, gin.H{"method": "indirect", "report": report})
		return
	}
	rows, err := a.Ledger.CashFlow(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build cash flow")
		return
	}
	c.JSON(http.StatusOK, gin.H{"method": "direct", "rows": rows})
}

func (a *API) FinanceSummary(c *gin.Context) {
	summary, err := a.Ledger.FinanceSummary(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build summary")
		return
	}
	c.JSON(http.StatusOK, summary)
}
