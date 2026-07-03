package handlers

import (
	"encoding/json"
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

type invoiceLineRequest struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice" binding:"required"`
	TaxCode     string `json:"taxCode"`
}

type createInvoiceRequest struct {
	CustomerRef string `json:"customerRef" binding:"required"`
	Currency    string `json:"currency"`
	DueDate     string `json:"dueDate"`
	Notes       string `json:"notes"`
	// Optional IFRS 15 recognition: method 'ratable' spreads the subtotal over
	// recognitionPeriods months from recognitionStart ('YYYY-MM').
	RecognitionMethod  string               `json:"recognitionMethod"`
	RecognitionPeriods int                  `json:"recognitionPeriods"`
	RecognitionStart   string               `json:"recognitionStart"`
	Lines              []invoiceLineRequest `json:"lines" binding:"required,min=1"`
}

// CreateInvoice builds a draft invoice with line items + tax (entity from header).
func (a *API) CreateInvoice(c *gin.Context) {
	var req createInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	var due *time.Time
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "dueDate must be YYYY-MM-DD")
			return
		}
		due = &t
	}
	lines := make([]repository.InvoiceLineInput, 0, len(req.Lines))
	for _, l := range req.Lines {
		qty := decimal.NewFromInt(1)
		if l.Quantity != "" {
			q, err := decimal.NewFromString(l.Quantity)
			if err != nil {
				apierr.JSONStatus(c, http.StatusBadRequest, "invalid quantity")
				return
			}
			qty = q
		}
		price, err := decimal.NewFromString(l.UnitPrice)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid unitPrice")
			return
		}
		lines = append(lines, repository.InvoiceLineInput{
			Description: l.Description, Quantity: qty, UnitPrice: price, TaxCode: l.TaxCode,
		})
	}
	inv, err := a.Ledger.CreateInvoice(c.Request.Context(), repository.CreateInvoiceInput{
		CustomerRef: req.CustomerRef, Currency: req.Currency, DueDate: due, Notes: req.Notes,
		RecognitionMethod: strings.TrimSpace(req.RecognitionMethod), RecognitionPeriods: req.RecognitionPeriods,
		RecognitionStart: strings.TrimSpace(req.RecognitionStart), Lines: lines,
	})
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create invoice")
		return
	}
	c.JSON(http.StatusCreated, inv)
}

func (a *API) ListBillingInvoices(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListInvoices(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list invoices")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) GetBillingInvoice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	inv, err := a.Ledger.GetInvoice(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not load invoice")
		return
	}
	if inv == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "invoice not found")
		return
	}
	c.JSON(http.StatusOK, inv)
}

type createRecurringRequest struct {
	CustomerRef string `json:"customerRef" binding:"required"`
	Currency    string `json:"currency"`
	Cadence     string `json:"cadence" binding:"required"` // weekly|monthly
	NextRun     string `json:"nextRun" binding:"required"` // YYYY-MM-DD
	Notes       string `json:"notes"`
	// Optional IFRS 15 recognition inherited by each generated invoice.
	RecognitionMethod  string               `json:"recognitionMethod"`
	RecognitionPeriods int                  `json:"recognitionPeriods"`
	Lines              []invoiceLineRequest `json:"lines" binding:"required,min=1"`
}

// CreateRecurringInvoice schedules a recurring invoice; a worker generates and
// issues an invoice each time it falls due.
func (a *API) CreateRecurringInvoice(c *gin.Context) {
	var req createRecurringRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Cadence != "weekly" && req.Cadence != "monthly" {
		apierr.JSONStatus(c, http.StatusBadRequest, "cadence must be weekly or monthly")
		return
	}
	next, err := time.Parse("2006-01-02", req.NextRun)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "nextRun must be YYYY-MM-DD")
		return
	}
	template, _ := json.Marshal(req.Lines)
	ri, err := a.Ledger.CreateRecurringInvoice(c.Request.Context(), repository.CreateRecurringInput{
		CustomerRef: req.CustomerRef, Currency: req.Currency, Cadence: req.Cadence,
		NextRun: next, Template: template, Notes: req.Notes,
		RecognitionMethod: strings.TrimSpace(req.RecognitionMethod), RecognitionPeriods: req.RecognitionPeriods,
	})
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create recurring invoice")
		return
	}
	c.JSON(http.StatusCreated, ri)
}

func (a *API) ListRecurringInvoices(c *gin.Context) {
	items, err := a.Ledger.ListRecurringInvoices(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list recurring invoices")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type createPaymentIntentRequest struct {
	OpenItemID string `json:"openItemId" binding:"required"`
	Amount     string `json:"amount" binding:"required"`
	Currency   string `json:"currency"`
}

// CreatePaymentIntent records an intent to collect on an AR open item.
func (a *API) CreatePaymentIntent(c *gin.Context) {
	var req createPaymentIntentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	itemID, err := uuid.Parse(req.OpenItemID)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "openItemId must be a UUID")
		return
	}
	amount, err := ledger.ParsePaymentAmount(req.Amount)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	pi, err := a.Ledger.CreatePaymentIntent(c.Request.Context(), itemID, amount, req.Currency)
	if err != nil {
		if errors.Is(err, ledger.ErrOpenItemNotFound) {
			apierr.JSONStatus(c, http.StatusNotFound, "open item not found")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not create payment intent")
		return
	}
	c.JSON(http.StatusCreated, pi)
}

// ConfirmPaymentIntent settles a pending intent (manual provider / webhook).
func (a *API) ConfirmPaymentIntent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	pi, err := a.Ledger.ConfirmPaymentIntent(c.Request.Context(), id, chainActor(c))
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrPaymentIntentNotFound):
			apierr.JSONStatus(c, http.StatusNotFound, "payment intent not found")
		case errors.Is(err, ledger.ErrPaymentExceeds):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "payment exceeds open balance")
		default:
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not confirm payment intent")
		}
		return
	}
	c.JSON(http.StatusOK, pi)
}

// IssueInvoice posts a draft invoice to AR + the GL.
func (a *API) IssueInvoice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	inv, err := a.Ledger.IssueInvoice(c.Request.Context(), id, chainActor(c))
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrInvoiceNotFound):
			apierr.JSONStatus(c, http.StatusNotFound, "invoice not found")
		case errors.Is(err, ledger.ErrInvoiceNotDraft):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "only a draft invoice can be issued")
		default:
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not issue invoice")
		}
		return
	}
	c.JSON(http.StatusOK, inv)
}
