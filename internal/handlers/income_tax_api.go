package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// IAS 12 income-tax endpoints.

type currentTaxRequest struct {
	Period        string `json:"period" binding:"required"`        // label / idempotency key
	TaxableProfit string `json:"taxableProfit" binding:"required"` // caller-computed
	Rate          string `json:"rate"`                             // fraction; default Uganda 30%
}

type deferredTaxRequest struct {
	Reference      string `json:"reference" binding:"required"`
	Description    string `json:"description"`
	TempDifference string `json:"tempDifference" binding:"required"`
	DType          string `json:"dtype" binding:"required"` // deductible | taxable
	Rate           string `json:"rate"`                     // fraction; default Uganda 30%
}

func taxRate(raw string) (decimal.Decimal, error) {
	if strings.TrimSpace(raw) == "" {
		return decimal.RequireFromString(repository.UgandaCorporateRate), nil
	}
	return decimal.NewFromString(strings.TrimSpace(raw))
}

// RunCurrentTax books the current tax provision for a period.
func (a *API) RunCurrentTax(c *gin.Context) {
	var req currentTaxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	profit, err := decimal.NewFromString(strings.TrimSpace(req.TaxableProfit))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid taxable profit")
		return
	}
	rate, err := taxRate(req.Rate)
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid rate")
		return
	}
	run, err := a.Ledger.RunCurrentTax(c.Request.Context(), strings.TrimSpace(req.Period), profit, rate, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, taxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, run)
}

// RecognizeDeferredTax books deferred tax on a temporary difference.
func (a *API) RecognizeDeferredTax(c *gin.Context) {
	var req deferredTaxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	diff, err := decimal.NewFromString(strings.TrimSpace(req.TempDifference))
	if err != nil || diff.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "temporary difference must be a positive amount")
		return
	}
	rate, err := taxRate(req.Rate)
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid rate")
		return
	}
	item, err := a.Ledger.RecognizeDeferredTax(c.Request.Context(), strings.TrimSpace(req.Reference), req.Description, diff, rate, strings.TrimSpace(req.DType), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, taxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, item)
}

// ListIncomeTaxRuns returns recent current-tax runs.
func (a *API) ListIncomeTaxRuns(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListIncomeTaxRuns(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list tax runs")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListDeferredTaxItems returns recent deferred-tax items.
func (a *API) ListDeferredTaxItems(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListDeferredTaxItems(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list deferred tax")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func taxErrStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrTaxRunExists):
		return http.StatusConflict
	case errors.Is(err, repository.ErrDeferredTaxExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
