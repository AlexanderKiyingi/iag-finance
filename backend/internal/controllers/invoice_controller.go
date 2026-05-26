package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/validate"
	"github.com/iag/finance-backend/internal/views"
)

type InvoiceController struct {
	store *models.Store
}

func NewInvoiceController(store *models.Store) *InvoiceController {
	return &InvoiceController{store: store}
}

func (c *InvoiceController) List(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	q := r.URL.Query()
	resp, _ := c.store.ListInvoices(q.Get("status"), q.Get("q"), p)
	views.JSON(w, http.StatusOK, resp)
}

func (c *InvoiceController) Get(w http.ResponseWriter, r *http.Request) {
	inv, err := c.store.GetInvoice(lastPathSegment(r))
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, inv)
}

func (c *InvoiceController) Create(w http.ResponseWriter, r *http.Request) {
	if !requirePerm(c.store, w, "invoices.write") {
		return
	}
	var in models.InvoiceInput
	if err := decodeJSON(r, &in); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validate.Required("customer", in.Customer); err != nil {
		views.WriteError(w, err)
		return
	}
	inv, err := c.store.CreateInvoice(in)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusCreated, inv)
}

func (c *InvoiceController) Patch(w http.ResponseWriter, r *http.Request) {
	if !requirePerm(c.store, w, "invoices.write") {
		return
	}
	var patch models.InvoicePatch
	if err := decodeJSON(r, &patch); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	inv, err := c.store.PatchInvoice(lastPathSegment(r), patch)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, inv)
}

func (c *InvoiceController) Delete(w http.ResponseWriter, r *http.Request) {
	if !requirePerm(c.store, w, "invoices.write") {
		return
	}
	if err := c.store.DeleteInvoice(lastPathSegment(r)); err != nil {
		views.WriteError(w, err)
		return
	}
	views.NoContent(w)
}

func (c *InvoiceController) Funnel(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, c.store.SalesFunnel())
}
