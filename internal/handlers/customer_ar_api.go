package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/pdf"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) CustomerStatement(c *gin.Context) {
	customerRef := strings.TrimSpace(c.Param("customerRef"))
	if customerRef == "" {
		apierr.JSONStatus(c, http.StatusBadRequest, "customerRef required")
		return
	}
	limit, offset := pagination(c)
	items, err := a.Ledger.ListARByCustomerRef(c.Request.Context(), customerRef, limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build statement")
		return
	}
	var opening, charges, payments, closing decimal.Decimal
	lines := make([]gin.H, 0, len(items))
	for _, it := range items {
		amt, _ := decimal.NewFromString(it.Amount)
		paid, _ := decimal.NewFromString(it.AmountPaid)
		balance := amt.Sub(paid)
		charges = charges.Add(amt)
		payments = payments.Add(paid)
		lines = append(lines, gin.H{
			"documentRef": it.DocumentRef,
			"description": it.Description,
			"amount":      it.Amount,
			"amountPaid":  it.AmountPaid,
			"balance":     balance.StringFixed(2),
			"currency":    it.Currency,
			"status":      it.Status,
			"dueDate":     formatDate(it.DueDate),
		})
	}
	closing = charges.Sub(payments)
	c.JSON(http.StatusOK, gin.H{
		"customerRef": customerRef,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"summary": gin.H{
			"openingBalance": opening.StringFixed(2),
			"charges":        charges.StringFixed(2),
			"payments":       payments.StringFixed(2),
			"closingBalance": closing.StringFixed(2),
		},
		"lines": lines,
	})
}

func (a *API) InvoicePDF(c *gin.Context) {
	docRef := strings.TrimSpace(c.Param("documentRef"))
	item, err := a.Ledger.GetARByDocumentRef(c.Request.Context(), docRef)
	if err != nil || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "invoice not found")
		return
	}
	data := pdf.InvoiceData{
		DocumentRef: item.DocumentRef,
		CustomerRef: item.CustomerRef,
		Description: item.Description,
		Amount:      item.Amount,
		AmountPaid:  item.AmountPaid,
		Currency:    item.Currency,
		Status:      item.Status,
		DueDate:     formatDate(item.DueDate),
		IssuedAt:    item.CreatedAt.UTC().Format("2006-01-02"),
	}
	body, err := pdf.RenderInvoice(data)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not render pdf")
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s.pdf"`, docRef))
	c.Data(http.StatusOK, "application/pdf", body)
}

type emailInvoiceRequest struct {
	Email string `json:"email"`
}

// EmailInvoice publishes an "invoice-ready-email" notification for an issued AR
// invoice. Recipient is the request body's email if given, else the customer's
// stored email (party master). Sending is handled by the notifications service
// off the iag.notifications topic; 202 means queued, not delivered.
func (a *API) EmailInvoice(c *gin.Context) {
	docRef := strings.TrimSpace(c.Param("documentRef"))
	item, err := a.Ledger.GetARByDocumentRef(c.Request.Context(), docRef)
	if err != nil || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "invoice not found")
		return
	}

	var req emailInvoiceRequest
	_ = c.ShouldBindJSON(&req)
	recipient := strings.TrimSpace(req.Email)
	if recipient == "" {
		recipient, _ = a.Ledger.CustomerEmailByRef(c.Request.Context(), item.CustomerRef)
	}
	if recipient == "" {
		apierr.JSONStatus(c, http.StatusUnprocessableEntity, "no recipient email — pass one or set the customer's email")
		return
	}
	if a.Events == nil || !a.Events.NotificationsEnabled() {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "email delivery is not configured")
		return
	}

	a.Events.PublishNotification(c.Request.Context(), recipient, "invoice-ready-email", map[string]string{
		"documentRef": item.DocumentRef,
		"amount":      item.Amount,
		"currency":    item.Currency,
	})
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "recipient": recipient, "documentRef": item.DocumentRef})
}

func (a *API) PaymentLink(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	item, err := a.Ledger.GetAROpenItemByID(c.Request.Context(), id)
	if err != nil || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "invoice not found")
		return
	}
	token, err := a.Ledger.EnsurePaymentLinkToken(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not create payment link")
		return
	}
	base := strings.TrimRight(a.Cfg.PaymentLinkBaseURL, "/")
	if base == "" {
		base = "/pay"
	}
	url := fmt.Sprintf("%s/%s", base, token)
	c.JSON(http.StatusOK, gin.H{
		"documentRef": item.DocumentRef,
		"customerRef": item.CustomerRef,
		"amount":      item.Amount,
		"amountPaid":  item.AmountPaid,
		"currency":    item.Currency,
		"paymentUrl":  url,
		"token":       token,
	})
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
