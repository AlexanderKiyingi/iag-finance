package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// Three-way match endpoints.

// RunMatchCheck classifies GR/IR accruals and raises exceptions.
func (a *API) RunMatchCheck(c *gin.Context) {
	open, err := a.Ledger.RunMatchCheck(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "match check failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"openExceptions": open})
}

// ListMatchExceptions returns the review queue (optional ?status=open|resolved).
func (a *API) ListMatchExceptions(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListMatchExceptions(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list match exceptions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ResolveMatchException marks an exception resolved.
func (a *API) ResolveMatchException(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid exception id")
		return
	}
	if err := a.Ledger.ResolveMatchException(c.Request.Context(), id, chainActor(c)); err != nil {
		if errors.Is(err, repository.ErrMatchExceptionNotFound) {
			apierr.JSONStatus(c, http.StatusNotFound, err.Error())
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "resolved"})
}

// WriteOffMatchVariance writes a PO's residual GR/IR balance to PPV.
func (a *API) WriteOffMatchVariance(c *gin.Context) {
	var body struct {
		PORef string `json:"poRef" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	entry, err := a.Ledger.WriteOffMatchVariance(c.Request.Context(), strings.TrimSpace(body.PORef), chainActor(c))
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ledger.ErrPeriodClosed):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, repository.ErrNoVariance):
			status = http.StatusUnprocessableEntity
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
}
