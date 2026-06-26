package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// legacyBill is the AP counterpart of legacyInvoice — a flat vendor-bill shape
// for simple CRUD clients, backed by ap_open_items.
type legacyBill struct {
	No      string  `json:"no"`
	Date    string  `json:"date"`
	Due     string  `json:"due"`
	Vendor  string  `json:"vendor"`
	Total   float64 `json:"total"`
	Balance float64 `json:"balance"`
	Status  string  `json:"status"`
}

func toLegacyBill(item domain.APOpenItem) (legacyBill, error) {
	total, err := ledger.BillTotal(item)
	if err != nil {
		return legacyBill{}, err
	}
	bal, err := ledger.BillBalance(item)
	if err != nil {
		return legacyBill{}, err
	}
	bill := legacyBill{
		No: item.DocumentRef, Vendor: item.VendorRef,
		Total: total, Balance: bal, Status: ledger.MapBillStatus(item),
	}
	if !item.CreatedAt.IsZero() {
		bill.Date = item.CreatedAt.Format("2006-01-02")
	}
	if item.DueDate != nil {
		bill.Due = item.DueDate.Format("2006-01-02")
	}
	return bill, nil
}

func (a *API) ListBillsLegacy(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListAPFiltered(c.Request.Context(), c.Query("status"), c.Query("q"), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list bills")
		return
	}
	out := make([]legacyBill, 0, len(items))
	for _, it := range items {
		bill, err := toLegacyBill(it)
		if err != nil {
			continue
		}
		out = append(out, bill)
	}
	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"meta":  gin.H{"total": len(out), "page": offset/limit + 1, "limit": limit},
	})
}

func (a *API) GetBillLegacy(c *gin.Context) {
	item, err := a.Ledger.GetAPByDocumentRef(c.Request.Context(), c.Param("no"))
	if err != nil || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	bill, err := toLegacyBill(*item)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not map bill")
		return
	}
	c.JSON(http.StatusOK, bill)
}

type legacyBillInput struct {
	Date   string  `json:"date"`
	Due    string  `json:"due"`
	Vendor string  `json:"vendor" binding:"required"`
	Total  float64 `json:"total" binding:"required"`
	Status string  `json:"status"`
}

func (a *API) CreateBillLegacy(c *gin.Context) {
	var in legacyBillInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	no := c.Query("no")
	if no == "" {
		no = "BILL-" + strconv.FormatInt(time.Now().Unix(), 10)
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
	// The invoice.posted event is written to the outbox in the same transaction
	// as the AP item, so the GL booking can never diverge if the broker is down.
	outbox := invoicePostedOutbox(a.Events, no, in.Vendor, amount, "UGX")
	item, err := a.Ledger.CreateAPItem(c.Request.Context(), in.Vendor, no, "", amount, "UGX", due, outbox)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create bill")
		return
	}
	bill, _ := toLegacyBill(*item)
	c.JSON(http.StatusCreated, bill)
}

type legacyBillPatch struct {
	Due    *string `json:"due"`
	Vendor *string `json:"vendor"`
	Status *string `json:"status"`
}

func (a *API) PatchBillLegacy(c *gin.Context) {
	var patch legacyBillPatch
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
	item, err := a.Ledger.UpdateAPByDocumentRef(c.Request.Context(), c.Param("no"), patch.Vendor, nil, due)
	if ledger.IsInvoiceNotFound(err) || item == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not update bill")
		return
	}
	bill, _ := toLegacyBill(*item)
	c.JSON(http.StatusOK, bill)
}

func (a *API) DeleteBillLegacy(c *gin.Context) {
	if err := a.Ledger.DeleteAPByDocumentRef(c.Request.Context(), c.Param("no")); err != nil {
		if ledger.IsInvoiceNotFound(err) {
			apierr.JSONStatus(c, http.StatusNotFound, "not found")
			return
		}
		apierr.JSONStatus(c, http.StatusConflict, "bill cannot be deleted")
		return
	}
	c.Status(http.StatusNoContent)
}
