package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) ListAuditLogs(c *gin.Context) {
	limit, offset := pagination(c)
	filter := auditlog.ListFilter{Limit: limit, Offset: offset}
	filter.EventType = c.Query("eventType")

	if actor := c.Query("actorId"); actor != "" {
		id, err := uuid.Parse(actor)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid actorId")
			return
		}
		filter.ActorID = &id
	}
	filter.Resource = c.Query("resource")

	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid from (RFC3339)")
			return
		}
		filter.From = &t
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid to (RFC3339)")
			return
		}
		filter.To = &t
	}

	items, total, err := a.Audit.List(c.Request.Context(), filter)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list audit logs")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

// ListOwnAuditLogs returns the caller's own audit trail.
//
// ListAuditLogs sits behind RequireAdmin because a full trail names everyone's
// actions. But a person seeing what *they* did needs no privilege — and without
// this the app's activity page could only ever 403 for ordinary staff, which
// reads as "nothing happened" rather than "you may not see this".
//
// Authorization here is the forced actor filter, not a permission: the caller's
// id comes from the verified token and overwrites any actorId in the query, so
// there is no parameter that widens the result beyond the caller.
func (a *API) ListOwnAuditLogs(c *gin.Context) {
	actor, ok := portalUserID(c)
	if !ok {
		apierr.JSONStatus(c, http.StatusUnauthorized, "authentication required")
		return
	}

	limit, offset := pagination(c)
	filter := auditlog.ListFilter{
		Limit:    limit,
		Offset:   offset,
		ActorID:  &actor,
		Resource: c.Query("resource"),
	}
	filter.EventType = c.Query("eventType")

	if from := c.Query("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid from (RFC3339)")
			return
		}
		filter.From = &t
	}
	if to := c.Query("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid to (RFC3339)")
			return
		}
		filter.To = &t
	}

	items, total, err := a.Audit.List(c.Request.Context(), filter)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list audit logs")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset, "scope": "self"})
}

func (a *API) GetAuditLog(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := a.Audit.Get(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not load audit entry")
		return
	}
	if entry == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	c.JSON(http.StatusOK, entry)
}
