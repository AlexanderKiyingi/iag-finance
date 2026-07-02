package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// IAS 37 provision endpoints (grouped under /provisions/liability, static paths
// with the provision id in the body to avoid a gin wildcard clash).

type recognizeProvisionRequest struct {
	Kind               string `json:"kind"`
	Description        string `json:"description"`
	Estimate           string `json:"estimate" binding:"required"`
	DiscountRate       string `json:"discountRate"`
	ExpectedSettlement string `json:"expectedSettlement"` // YYYY-MM-DD
	Currency           string `json:"currency"`
	AssetRef           string `json:"assetRef"`
}

type provisionOpRequest struct {
	ID        string `json:"id" binding:"required"`
	Amount    string `json:"amount"`    // utilize / remeasure(new estimate)
	Reference string `json:"reference"` // idempotency key
}

// RecognizeProvision registers and books a new provision.
func (a *API) RecognizeProvision(c *gin.Context) {
	var req recognizeProvisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	estimate, err := decimal.NewFromString(strings.TrimSpace(req.Estimate))
	if err != nil || estimate.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "estimate must be a positive amount")
		return
	}
	rate := decimal.Zero
	if s := strings.TrimSpace(req.DiscountRate); s != "" {
		rate, err = decimal.NewFromString(s)
		if err != nil || rate.IsNegative() {
			apierr.JSONStatus(c, http.StatusBadRequest, "discountRate must be a non-negative fraction")
			return
		}
	}
	var settlement *time.Time
	if s := strings.TrimSpace(req.ExpectedSettlement); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "expectedSettlement must be YYYY-MM-DD")
			return
		}
		settlement = &t
	}
	prov, err := a.Ledger.RecognizeProvision(c.Request.Context(), repository.RecognizeProvisionInput{
		Kind: req.Kind, Description: req.Description, Estimate: estimate, DiscountRate: rate,
		ExpectedSettlement: settlement, Currency: req.Currency, AssetRef: strings.TrimSpace(req.AssetRef),
	}, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionOpStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, prov)
}

// UnwindProvision accrues one period's discount unwind.
func (a *API) UnwindProvision(c *gin.Context) {
	id, req, ok := a.bindProvisionOp(c)
	if !ok {
		return
	}
	prov, err := a.Ledger.UnwindProvision(c.Request.Context(), id, strings.TrimSpace(req.Reference), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionOpStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, prov)
}

// UtilizeProvision settles part of a provision against cash.
func (a *API) UtilizeProvision(c *gin.Context) {
	id, req, ok := a.bindProvisionOp(c)
	if !ok {
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "amount must be a positive amount")
		return
	}
	prov, err := a.Ledger.UtilizeProvision(c.Request.Context(), id, amount, strings.TrimSpace(req.Reference), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionOpStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, prov)
}

// RemeasureProvision adjusts the provision to a new estimate (in the amount field).
func (a *API) RemeasureProvision(c *gin.Context) {
	id, req, ok := a.bindProvisionOp(c)
	if !ok {
		return
	}
	newEstimate, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || newEstimate.IsNegative() {
		apierr.JSONStatus(c, http.StatusBadRequest, "amount (new estimate) must be a non-negative amount")
		return
	}
	prov, err := a.Ledger.RemeasureProvision(c.Request.Context(), id, newEstimate, strings.TrimSpace(req.Reference), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionOpStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, prov)
}

// ReverseProvision releases an unused provision.
func (a *API) ReverseProvision(c *gin.Context) {
	id, req, ok := a.bindProvisionOp(c)
	if !ok {
		return
	}
	prov, err := a.Ledger.ReverseProvision(c.Request.Context(), id, strings.TrimSpace(req.Reference), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, provisionOpStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, prov)
}

// ListProvisions returns recent provisions.
func (a *API) ListProvisions(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListProvisions(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list provisions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) bindProvisionOp(c *gin.Context) (uuid.UUID, provisionOpRequest, bool) {
	var req provisionOpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return uuid.Nil, req, false
	}
	id, err := uuid.Parse(strings.TrimSpace(req.ID))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid provision id")
		return uuid.Nil, req, false
	}
	return id, req, true
}

func provisionOpStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrProvisionNotFound):
		return http.StatusNotFound
	case errors.Is(err, repository.ErrProvisionClosed):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
