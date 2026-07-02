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

// IFRS 16 lease endpoints.

type createLeaseRequest struct {
	LeaseRef       string `json:"leaseRef" binding:"required"`
	Description    string `json:"description"`
	Currency       string `json:"currency"`
	MonthlyPayment string `json:"monthlyPayment" binding:"required"`
	AnnualRate     string `json:"annualRate"` // e.g. "0.12" for 12%/yr; default 0
	TermMonths     int    `json:"termMonths" binding:"required"`
	StartPeriod    string `json:"startPeriod"` // YYYY-MM
}

type leaseRunRequest struct {
	Period string `json:"period" binding:"required"` // YYYY-MM
}

// CreateLease recognises a lease and stores its amortization schedule.
func (a *API) CreateLease(c *gin.Context) {
	var req createLeaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	payment, err := decimal.NewFromString(req.MonthlyPayment)
	if err != nil || payment.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid monthly payment")
		return
	}
	rate := decimal.Zero
	if strings.TrimSpace(req.AnnualRate) != "" {
		rate, err = decimal.NewFromString(req.AnnualRate)
		if err != nil || rate.IsNegative() {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid annual rate")
			return
		}
	}
	in := repository.CreateLeaseInput{
		LeaseRef:       strings.TrimSpace(req.LeaseRef),
		Description:    req.Description,
		Currency:       req.Currency,
		MonthlyPayment: payment,
		AnnualRate:     rate,
		TermMonths:     req.TermMonths,
		StartPeriod:    strings.TrimSpace(req.StartPeriod),
	}
	lease, err := a.Ledger.CreateLease(c.Request.Context(), in, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, leaseErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, lease)
}

// RunLeasePeriod books interest, payment and depreciation for a period.
func (a *API) RunLeasePeriod(c *gin.Context) {
	var req leaseRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Ledger.RunLeasePeriod(c.Request.Context(), strings.TrimSpace(req.Period), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, leaseErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListLeases returns recent leases.
func (a *API) ListLeases(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListLeases(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list leases")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func leaseErrStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrLeaseExists):
		return http.StatusConflict
	case errors.Is(err, repository.ErrLeaseNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
