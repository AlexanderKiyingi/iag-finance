package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

type adjustmentRequest struct {
	OriginalDocumentRef string `json:"originalDocumentRef" binding:"required"`
	DocumentRef         string `json:"documentRef"`
	Amount              string `json:"amount" binding:"required"`
	Currency            string `json:"currency"`
	Reason              string `json:"reason"`
}

func (a *API) CreateARCreditNote(c *gin.Context) {
	a.createAdjustment(c, "ar", "credit_note")
}

func (a *API) CreateARDebitNote(c *gin.Context) {
	a.createAdjustment(c, "ar", "debit_note")
}

func (a *API) CreateAPCreditNote(c *gin.Context) {
	a.createAdjustment(c, "ap", "credit_note")
}

func (a *API) CreateAPDebitNote(c *gin.Context) {
	a.createAdjustment(c, "ap", "debit_note")
}

func (a *API) ListAdjustments(c *gin.Context) {
	limit, offset := pagination(c)
	_ = offset
	items, err := a.Ledger.ListAdjustments(c.Request.Context(), c.Query("originalDocumentRef"), c.Query("direction"), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list adjustments")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) createAdjustment(c *gin.Context, direction, kind string) {
	var req adjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid amount")
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "UGX"
	}
	adj, err := a.Ledger.CreateAdjustment(c.Request.Context(), ledger.AdjustmentInput{
		Kind:                kind,
		Direction:           direction,
		OriginalDocumentRef: req.OriginalDocumentRef,
		DocumentRef:         req.DocumentRef,
		Reason:              req.Reason,
		Amount:              amount,
		Currency:            currency,
	}, chainActor(c))
	if err != nil {
		status := http.StatusConflict
		switch {
		case errors.Is(err, ledger.ErrInvalidAdjustment):
			status = http.StatusBadRequest
		case errors.Is(err, repository.ErrAdjustmentTooLarge):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, ledger.ErrPeriodClosed):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, repository.ErrOriginalNotFound):
			status = http.StatusNotFound
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, adj)
}
