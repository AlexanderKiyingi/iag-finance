package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/views"
)

type ApprovalController struct {
	store *models.Store
}

func NewApprovalController(store *models.Store) *ApprovalController {
	return &ApprovalController{store: store}
}

func (c *ApprovalController) List(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	items, pg := c.store.ListApprovals(p)
	views.JSON(w, http.StatusOK, listResponse(items, pg))
}

func (c *ApprovalController) Get(w http.ResponseWriter, r *http.Request) {
	a, err := c.store.GetApproval(lastPathSegment(r))
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, a)
}

func (c *ApprovalController) Patch(w http.ResponseWriter, r *http.Request) {
	if !requirePerm(c.store, w, "approvals.write") {
		return
	}
	var patch models.ApprovalPatch
	if err := decodeJSON(r, &patch); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	a, err := c.store.PatchApproval(lastPathSegment(r), patch)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, a)
}
