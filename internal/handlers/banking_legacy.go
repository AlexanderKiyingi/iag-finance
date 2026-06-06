package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) ListBankingAccounts(c *gin.Context) {
	items, err := a.Ledger.ListLegacyBankAccounts(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list bank accounts")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "meta": gin.H{"total": len(items)}})
}

func (a *API) ListBankingTransactions(c *gin.Context) {
	limit, offset := pagination(c)
	items, total, err := a.Ledger.ListLegacyBankTx(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list transactions")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"meta":  gin.H{"total": total, "page": offset/limit + 1, "limit": limit},
	})
}

type syncBankFeedRequest struct {
	BankAccountCode string `json:"bankAccountCode" binding:"required"`
	FromDate        string `json:"fromDate"`
	ToDate          string `json:"toDate"`
}

func (a *API) SyncBankFeed(c *gin.Context) {
	if a.Integrations == nil || a.Integrations.Bank == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "bank feed not configured")
		return
	}
	var req syncBankFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -7)
	if req.ToDate != "" {
		t, err := time.Parse("2006-01-02", req.ToDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid toDate")
			return
		}
		to = t
	}
	if req.FromDate != "" {
		t, err := time.Parse("2006-01-02", req.FromDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid fromDate")
			return
		}
		from = t
	}
	lines, err := a.Integrations.Bank.FetchLines(c.Request.Context(), req.BankAccountCode, from, to)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadGateway, err.Error())
		return
	}
	inputs := make([]repository.StatementLineInput, 0, len(lines))
	for _, l := range lines {
		inputs = append(inputs, repository.StatementLineInput{
			Date: l.Date, Description: l.Description, Payee: l.Payee,
			Amount: l.Amount, Direction: l.Direction, ExternalRef: l.ExternalRef,
		})
	}
	stmtID, n, err := a.Ledger.SyncBankFeed(c.Request.Context(), req.BankAccountCode, from, to, inputs)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not persist bank feed")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"statementId": stmtID,
		"lines":       n,
		"adapter":     a.Integrations.Bank.Mode(),
	})
}
