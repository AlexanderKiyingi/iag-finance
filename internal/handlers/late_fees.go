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

// ListLateFees backs GET /ar/late-fees.
func (a *API) ListLateFees(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListLateFees(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list late fees")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RecordLateFee backs POST /ar/late-fees.
func (a *API) RecordLateFee(c *gin.Context) {
	var body struct {
		FeeRef           string `json:"feeRef"`
		Customer         string `json:"customer"`
		InvoiceReference string `json:"invoiceReference"`
		Rate             string `json:"rate"`
		Amount           string `json:"amount"`
		FeeDate          string `json:"feeDate"`
		Currency         string `json:"currency"`
		Notes            string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(body.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "amount must be a positive fee amount")
		return
	}
	rate := decimal.Zero
	if s := strings.TrimSpace(body.Rate); s != "" {
		if rate, err = decimal.NewFromString(s); err != nil || rate.IsNegative() {
			apierr.JSONStatus(c, http.StatusBadRequest, "rate must be a non-negative percentage")
			return
		}
	}
	feeDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.FeeDate))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "feeDate must be YYYY-MM-DD")
		return
	}
	fee, err := a.Ledger.RecordLateFee(c.Request.Context(), repository.CreateLateFeeInput{
		FeeRef: strings.TrimSpace(body.FeeRef), Customer: strings.TrimSpace(body.Customer),
		InvoiceReference: strings.TrimSpace(body.InvoiceReference), Rate: rate, Amount: amount,
		FeeDate: feeDate.UTC(), Currency: strings.TrimSpace(body.Currency), Notes: strings.TrimSpace(body.Notes),
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			apierr.JSONStatus(c, http.StatusConflict, "a late fee with this reference already exists")
			return
		}
		if errors.Is(err, ledger.ErrPeriodClosed) {
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "the fee period is closed")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not record late fee")
		return
	}
	c.JSON(http.StatusCreated, fee)
}
