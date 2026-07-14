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

// ListWHTReceipts backs GET /tax/withholding.
func (a *API) ListWHTReceipts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListWHTReceipts(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list withholding-tax receipts")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RecordWHTReceipt backs POST /tax/withholding — records a WHT certificate and
// posts Dr 1150 WHT Recoverable / Cr 1100 AR.
func (a *API) RecordWHTReceipt(c *gin.Context) {
	var body struct {
		CertificateRef   string `json:"certificateRef"`
		Customer         string `json:"customer"`
		InvoiceReference string `json:"invoiceReference"`
		TaxAuthority     string `json:"taxAuthority"`
		Amount           string `json:"amount"`
		ReceiptDate      string `json:"receiptDate"`
		Currency         string `json:"currency"`
		Notes            string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(body.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "amount must be a positive withheld amount")
		return
	}
	receipt, err := time.Parse("2006-01-02", strings.TrimSpace(body.ReceiptDate))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "receiptDate must be YYYY-MM-DD")
		return
	}
	rec, err := a.Ledger.RecordWHTReceipt(c.Request.Context(), repository.CreateWHTReceiptInput{
		CertificateRef: strings.TrimSpace(body.CertificateRef), Customer: strings.TrimSpace(body.Customer),
		InvoiceReference: strings.TrimSpace(body.InvoiceReference), TaxAuthority: strings.TrimSpace(body.TaxAuthority),
		Amount: amount, ReceiptDate: receipt.UTC(), Currency: strings.TrimSpace(body.Currency),
		Notes: strings.TrimSpace(body.Notes),
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			apierr.JSONStatus(c, http.StatusConflict, "a WHT receipt with this certificate number already exists")
			return
		}
		if errors.Is(err, ledger.ErrPeriodClosed) {
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "the receipt period is closed")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not record withholding-tax receipt")
		return
	}
	c.JSON(http.StatusCreated, rec)
}
