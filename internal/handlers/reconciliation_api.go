package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) ListStatementLines(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid statement id")
		return
	}
	lines, err := a.Ledger.ListStatementLines(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list statement lines")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": lines})
}

type matchLineRequest struct {
	DocumentRef string `json:"documentRef" binding:"required"`
}

func (a *API) MatchStatementLine(c *gin.Context) {
	lineID, err := uuid.Parse(c.Param("lineId"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid line id")
		return
	}
	var req matchLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Ledger.MatchStatementLine(c.Request.Context(), lineID, req.DocumentRef); err != nil {
		status := http.StatusConflict
		if errors.Is(err, repository.ErrStatementLineNotFound) {
			status = http.StatusNotFound
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "matched", "documentRef": req.DocumentRef})
}

func (a *API) AutoReconcileStatement(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid statement id")
		return
	}
	n, err := a.Ledger.AutoMatchStatement(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"matched": n, "status": "reconciling"})
}
