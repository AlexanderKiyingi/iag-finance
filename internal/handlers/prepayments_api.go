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

// IAS 1 matching — prepaid-expense amortization endpoints.

type createPrepaymentRequest struct {
	SourceRef   string `json:"sourceRef" binding:"required"`
	Total       string `json:"total" binding:"required"`
	Currency    string `json:"currency"`
	ExpenseCode string `json:"expenseCode"` // account amortized into (default 5100)
	FundingCode string `json:"fundingCode"` // account credited at capitalization (default 1000)
	StartPeriod string `json:"startPeriod"` // YYYY-MM
	Periods     int    `json:"periods"`
}

type amortizationRunRequest struct {
	Period string `json:"period" binding:"required"` // YYYY-MM
}

// CreatePrepayment capitalises a prepayment and schedules its amortization.
func (a *API) CreatePrepayment(c *gin.Context) {
	var req createPrepaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	total, err := decimal.NewFromString(req.Total)
	if err != nil || total.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid total")
		return
	}
	in := repository.CreatePrepaymentInput{
		SourceRef:   strings.TrimSpace(req.SourceRef),
		Total:       total,
		Currency:    req.Currency,
		ExpenseCode: strings.TrimSpace(req.ExpenseCode),
		FundingCode: strings.TrimSpace(req.FundingCode),
		StartPeriod: strings.TrimSpace(req.StartPeriod),
		Periods:     req.Periods,
	}
	sched, err := a.Ledger.CreatePrepayment(c.Request.Context(), in, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, prepaymentErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, sched)
}

// RunAmortization expenses due straight-line slices for a period.
func (a *API) RunAmortization(c *gin.Context) {
	var req amortizationRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Ledger.RunAmortization(c.Request.Context(), strings.TrimSpace(req.Period), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, prepaymentErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListPrepayments returns recent schedules.
func (a *API) ListPrepayments(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListPrepayments(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list prepayments")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func prepaymentErrStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrPrepaymentExists):
		return http.StatusConflict
	case errors.Is(err, repository.ErrPrepaymentNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
