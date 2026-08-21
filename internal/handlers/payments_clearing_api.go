package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
)

// The Payments Clearing (1050) worklist.
//
// Migration 056 routed every operational disbursement through this control
// account rather than guessing what it paid for, on the stated understanding
// that finance would reconcile it against the originating document. Nothing
// existed to do that with: the account had a balance and no list. These
// endpoints are that list, and the matching act that empties it.

// ListPaymentsClearing returns the disbursements sitting in 1050.
//
// ?status=open (the default) is the worklist; ?status= returns everything
// including already-matched rows, for audit.
func (a *API) ListPaymentsClearing(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	status := "open"
	if raw, ok := c.GetQuery("status"); ok {
		status = strings.TrimSpace(raw)
	}
	switch status {
	case "", "open", "cleared":
	default:
		apierr.JSONStatus(c, http.StatusBadRequest, "status must be 'open', 'cleared' or empty")
		return
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := a.Ledger.ListPaymentsClearing(c.Request.Context(), scope, status, limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list payments clearing items")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type clearPaymentBody struct {
	// DocumentRef is the document this disbursement paid — a vendor invoice, a
	// payroll run, a loan agreement, a claim. Required: a cleared row that does
	// not say what cleared it defeats the purpose of the account, and the
	// database refuses it anyway.
	DocumentRef string `json:"documentRef"`
}

// ClearPaymentsClearingItem records which document a settled disbursement paid.
//
// It deliberately posts no journal. Reclassifying money out of 1050 into its
// final account is a ledger act with its own gates and period controls; this
// records the match that would justify one. Keeping them separate means an
// operator working the queue cannot move the general ledger as a side-effect of
// tidying a worklist.
func (a *API) ClearPaymentsClearingItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid clearing item id")
		return
	}
	var body clearPaymentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	documentRef := strings.TrimSpace(body.DocumentRef)
	if documentRef == "" {
		apierr.JSONStatus(c, http.StatusBadRequest, "documentRef is required")
		return
	}

	var by *uuid.UUID
	if raw, ok := c.Get("userID"); ok {
		if uid, ok := raw.(uuid.UUID); ok {
			by = &uid
		}
	}

	item, err := a.Ledger.ClearPaymentsClearingItem(c.Request.Context(), id, documentRef, by)
	if err != nil {
		if errors.Is(err, repository.ErrClearingItemNotFound) {
			// Either it never existed or someone already matched it. Both mean
			// "not yours to clear", and neither should overwrite a prior match.
			apierr.JSONStatus(c, http.StatusNotFound, "clearing item not found or already cleared")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not clear the item")
		return
	}
	c.JSON(http.StatusOK, item)
}
