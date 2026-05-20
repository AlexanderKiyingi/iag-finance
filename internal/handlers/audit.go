package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/auditlog"
)

func (a *API) ListAuditLogs(c *gin.Context) {
	limit, offset := pagination(c)
	filter := auditlog.ListFilter{Limit: limit, Offset: offset}
	filter.EventType = c.Query("eventType")

	if actor := c.Query("actorId"); actor != "" {
		id, err := uuid.Parse(actor)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid actorId"})
			return
		}
		filter.ActorID = &id
	}
	filter.Resource = c.Query("resource")

	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from (RFC3339)"})
			return
		}
		filter.From = &t
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to (RFC3339)"})
			return
		}
		filter.To = &t
	}

	items, total, err := a.Audit.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list audit logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (a *API) GetAuditLog(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	entry, err := a.Audit.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load audit entry"})
		return
	}
	if entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, entry)
}
