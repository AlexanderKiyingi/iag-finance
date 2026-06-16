package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
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
