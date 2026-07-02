package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// ListExchangeRates returns the configured FX rates and the base currency.
func (a *API) ListExchangeRates(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	rates, err := a.Ledger.ListExchangeRates(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list exchange rates")
		return
	}
	c.JSON(http.StatusOK, gin.H{"baseCurrency": a.Ledger.BaseCurrency(), "rates": rates})
}

type upsertRateRequest struct {
	Currency string `json:"currency" binding:"required"`
	Rate     string `json:"rate" binding:"required"`
	AsOfDate string `json:"asOfDate"`
}

// UpsertExchangeRate records a currency→base rate effective on a date (today if
// omitted). The rate must be a positive decimal.
func (a *API) UpsertExchangeRate(c *gin.Context) {
	var req upsertRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	rate, err := decimal.NewFromString(req.Rate)
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "rate must be a positive number")
		return
	}
	asOf := time.Now().UTC()
	if req.AsOfDate != "" {
		t, err := time.Parse("2006-01-02", req.AsOfDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "asOfDate must be YYYY-MM-DD")
			return
		}
		asOf = t
	}
	if err := a.Ledger.UpsertExchangeRate(c.Request.Context(), strings.ToUpper(req.Currency), rate.String(), asOf); err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not save exchange rate")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "saved"})
}

// RevalueFX posts a period-end unrealized FX revaluation (?period=YYYY-MM) of
// open foreign AR/AP, with an auto-reversal dated the next period. Idempotent.
func (a *API) RevalueFX(c *gin.Context) {
	period := c.Query("period")
	res, err := a.Ledger.RevalueFX(c.Request.Context(), period, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListTaxCodes returns the configured VAT/GST codes.
func (a *API) ListTaxCodes(c *gin.Context) {
	codes, err := a.Ledger.ListTaxCodes(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list tax codes")
		return
	}
	c.JSON(http.StatusOK, gin.H{"taxCodes": codes})
}

type upsertTaxCodeRequest struct {
	Code          string `json:"code" binding:"required"`
	Name          string `json:"name" binding:"required"`
	Rate          string `json:"rate" binding:"required"`
	Active        *bool  `json:"active"`
	ReverseCharge bool   `json:"reverseCharge"`
}

// UpsertTaxCode creates or updates a VAT/GST code (rate as a fraction, e.g. 0.18).
func (a *API) UpsertTaxCode(c *gin.Context) {
	var req upsertTaxCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	rate, err := decimal.NewFromString(req.Rate)
	if err != nil || rate.IsNegative() {
		apierr.JSONStatus(c, http.StatusBadRequest, "rate must be a non-negative fraction (e.g. 0.18)")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	if err := a.Ledger.UpsertTaxCode(c.Request.Context(), strings.ToUpper(req.Code), req.Name, rate.String(), active, req.ReverseCharge); err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not save tax code")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "saved"})
}

type reverseChargeRequest struct {
	TaxCode   string `json:"taxCode" binding:"required"`
	Reference string `json:"reference" binding:"required"`
	NetAmount string `json:"netAmount" binding:"required"`
}

// SelfAssessReverseCharge books the buyer's reverse-charge VAT (Dr input / Cr
// output) on a net purchase amount.
func (a *API) SelfAssessReverseCharge(c *gin.Context) {
	var req reverseChargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	net, err := decimal.NewFromString(strings.TrimSpace(req.NetAmount))
	if err != nil || net.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "netAmount must be a positive amount")
		return
	}
	entry, err := a.Ledger.SelfAssessReverseCharge(c.Request.Context(), strings.ToUpper(strings.TrimSpace(req.TaxCode)), strings.TrimSpace(req.Reference), net, chainActor(c))
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ledger.ErrPeriodClosed):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, repository.ErrNotReverseCharge):
			status = http.StatusBadRequest
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// VATReturn reports output − input VAT for a period (?from=&to=).
func (a *API) VATReturn(c *gin.Context) {
	res, err := a.Ledger.VATReturn(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build VAT return")
		return
	}
	c.JSON(http.StatusOK, res)
}
