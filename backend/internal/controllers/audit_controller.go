package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/views"
)

type AuditController struct {
	store *models.Store
}

func NewAuditController(store *models.Store) *AuditController {
	return &AuditController{store: store}
}

func (c *AuditController) List(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListAudit(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *AuditController) Create(w http.ResponseWriter, r *http.Request) {
	if !requirePerm(c.store, w, "audit.write") {
		return
	}
	var in models.AuditEntry
	if err := decodeJSON(r, &in); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	entry := c.store.AppendAuditEntry(in)
	views.JSON(w, http.StatusCreated, entry)
}
