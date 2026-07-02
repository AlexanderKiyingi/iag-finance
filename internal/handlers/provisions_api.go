package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// IFRS 9 provisioning endpoints. Grouped under /provisions to avoid a gin
// wildcard clash with the static /ar/items|invoices|customers children.

type eclRunRequest struct {
	Period string `json:"period" binding:"required"` // YYYY-MM
}

type writeOffRequest struct {
	DocumentRef string `json:"documentRef" binding:"required"`
	Reason      string `json:"reason"`
}

type recoverRequest struct {
	DocumentRef string `json:"documentRef" binding:"required"`
	Reference   string `json:"reference" binding:"required"`
	Amount      string `json:"amount" binding:"required"`
}

// RunECLProvision computes and books the expected-credit-loss allowance movement
// for a period.
func (a *API) RunECLProvision(c *gin.Context) {
	var req eclRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Ledger.RunECLProvision(c.Request.Context(), strings.TrimSpace(req.Period), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// WriteOffReceivable de-recognises an open receivable.
func (a *API) WriteOffReceivable(c *gin.Context) {
	var req writeOffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Ledger.WriteOffAR(c.Request.Context(), strings.TrimSpace(req.DocumentRef), req.Reason, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, res)
}

// RecoverReceivable books cash recovered on a written-off debt.
func (a *API) RecoverReceivable(c *gin.Context) {
	var req recoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid amount")
		return
	}
	entry, err := a.Ledger.RecoverAR(c.Request.Context(), strings.TrimSpace(req.DocumentRef), strings.TrimSpace(req.Reference), amount, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// ListECLProvisions returns recent provisioning runs.
func (a *API) ListECLProvisions(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListECLProvisions(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list provisions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func provisionErrStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrNothingToWriteOff):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrOriginalNotFound):
		return http.StatusNotFound
	case errors.Is(err, repository.ErrWriteOffNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
