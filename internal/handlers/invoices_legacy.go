package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// normalizeOpenItemStatus validates a manual AR/AP status override against the
// canonical lifecycle set. Empty/nil → (nil, true) so the column is left
// untouched (COALESCE no-op). An unrecognized value → (nil, false) so the
// handler can 400. Manual status is decoupled from amount_paid by design.
func normalizeOpenItemStatus(raw *string) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	s := strings.ToLower(strings.TrimSpace(*raw))
	if s == "" {
		return nil, true
	}
	switch s {
	case "open", "partial", "closed":
		return &s, true
	default:
		return nil, false
	}
}

type legacyInvoice struct {
	No       string  `json:"no"`
	Date     string  `json:"date"`
	Due      string  `json:"due"`
	Customer string  `json:"customer"`
	Total    float64 `json:"total"`
	Balance  float64 `json:"balance"`
	Status   string  `json:"status"`
}

func toLegacyInvoice(item domain.AROpenItem) (legacyInvoice, error) {
	total, err := ledger.InvoiceTotal(item)
	if err != nil {
		return legacyInvoice{}, err
	}
	bal, err := ledger.InvoiceBalance(item)
	if err != nil {
		return legacyInvoice{}, err
	}
	inv := legacyInvoice{
		No: item.DocumentRef, Customer: item.CustomerRef,
		Total: total, Balance: bal, Status: ledger.MapInvoiceStatus(item),
	}
	if !item.CreatedAt.IsZero() {
		inv.Date = item.CreatedAt.Format("2006-01-02")
	}
	if item.DueDate != nil {
		inv.Due = item.DueDate.Format("2006-01-02")
	}
	return inv, nil
}

func (a *API) ListInvoicesLegacy(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListARFiltered(c.Request.Context(), c.Query("status"), c.Query("q"), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list invoices")
		return
	}
	out := make([]legacyInvoice, 0, len(items))
	for _, it := range items {
		inv, err := toLegacyInvoice(it)
		if err != nil {
			continue
		}
		out = append(out, inv)
	}
	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"meta":  gin.H{"total": len(out), "page": offset/limit + 1, "limit": limit},
	})
}

func (a *API) GetInvoiceLegacy(c *gin.Context) {
	item, err := a.Ledger.GetARByDocumentRef(c.Request.Context(), c.Param("no"))
	if err != nil || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	inv, err := toLegacyInvoice(*item)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not map invoice")
		return
	}
	c.JSON(http.StatusOK, inv)
}

type legacyInvoiceInput struct {
	Date     string  `json:"date"`
	Due      string  `json:"due"`
	Customer string  `json:"customer" binding:"required"`
	Total    float64 `json:"total" binding:"required"`
	Status   string  `json:"status"`
}

func (a *API) CreateInvoiceLegacy(c *gin.Context) {
	var in legacyInvoiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	no := c.Query("no")
	if no == "" {
		no = "INV-" + strconv.FormatInt(time.Now().Unix(), 10)
	}
	var due *time.Time
	if in.Due != "" {
		t, err := time.Parse("2006-01-02", in.Due)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid due date")
			return
		}
		due = &t
	}
	amount := strconv.FormatFloat(in.Total, 'f', 2, 64)
	outbox := saleCompletedOutbox(a.Events, no, in.Customer, amount, "UGX")
	item, err := a.Ledger.CreateARItem(c.Request.Context(), in.Customer, no, "", amount, "UGX", due, outbox)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create invoice")
		return
	}
	inv, _ := toLegacyInvoice(*item)
	c.JSON(http.StatusCreated, inv)
}

type legacyInvoicePatch struct {
	Due      *string  `json:"due"`
	Customer *string  `json:"customer"`
	Status   *string  `json:"status"`
}

func (a *API) PatchInvoiceLegacy(c *gin.Context) {
	var patch legacyInvoicePatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	var due *time.Time
	if patch.Due != nil && *patch.Due != "" {
		t, err := time.Parse("2006-01-02", *patch.Due)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid due date")
			return
		}
		due = &t
	}
	status, ok := normalizeOpenItemStatus(patch.Status)
	if !ok {
		apierr.JSONStatus(c, http.StatusBadRequest, "status must be open, partial, or closed")
		return
	}
	item, err := a.Ledger.UpdateARByDocumentRef(c.Request.Context(), c.Param("no"), patch.Customer, nil, due, status)
	if ledger.IsInvoiceNotFound(err) || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not update invoice")
		return
	}
	inv, _ := toLegacyInvoice(*item)
	c.JSON(http.StatusOK, inv)
}

func (a *API) DeleteInvoiceLegacy(c *gin.Context) {
	if err := a.Ledger.DeleteARByDocumentRef(c.Request.Context(), c.Param("no")); err != nil {
		if ledger.IsInvoiceNotFound(err) {
			apierr.JSONStatus(c, http.StatusNotFound, "not found")
			return
		}
		apierr.JSONStatus(c, http.StatusConflict, "invoice cannot be deleted")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) InvoiceFunnel(c *gin.Context) {
	f, err := a.Ledger.SalesFunnel(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build funnel")
		return
	}
	c.JSON(http.StatusOK, f)
}
