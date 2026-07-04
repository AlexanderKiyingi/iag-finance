package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// ListEntities returns the configured accounting entities. Select one per request
// with the `X-Entity-Id` header; reports accept `?consolidated=true` to roll up an
// entity and its children.
func (a *API) ListEntities(c *gin.Context) {
	items, err := a.Ledger.Entities(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list entities")
		return
	}
	c.JSON(http.StatusOK, gin.H{"entities": items})
}

type createEntityRequest struct {
	Code         string `json:"code" binding:"required"`
	Name         string `json:"name" binding:"required"`
	BaseCurrency string `json:"baseCurrency"`
	ParentID     string `json:"parentId"`
	// OwnershipPct is the parent's ownership fraction of this entity (0<pct<=1,
	// default 1.0). Drives consolidation elimination + NCI.
	OwnershipPct string `json:"ownershipPct"`
}

// CreateEntity registers a new accounting entity (optionally under a parent for
// consolidation).
func (a *API) CreateEntity(c *gin.Context) {
	var req createEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	var parent *uuid.UUID
	if req.ParentID != "" {
		id, err := uuid.Parse(req.ParentID)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "parentId must be a UUID")
			return
		}
		parent = &id
	}
	e, err := a.Ledger.CreateEntity(c.Request.Context(), strings.ToUpper(req.Code), req.Name, strings.ToUpper(req.BaseCurrency), parent, strings.TrimSpace(req.OwnershipPct))
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create entity")
		return
	}
	c.JSON(http.StatusCreated, e)
}

type setOwnershipRequest struct {
	OwnershipPct string `json:"ownershipPct" binding:"required"` // 0 < pct <= 1
}

// SetEntityOwnership updates the parent's ownership fraction of an entity.
func (a *API) SetEntityOwnership(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid entity id")
		return
	}
	var req setOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	e, err := a.Ledger.SetEntityOwnership(c.Request.Context(), id, strings.TrimSpace(req.OwnershipPct))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not update ownership")
		return
	}
	if e == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "entity not found")
		return
	}
	c.JSON(http.StatusOK, e)
}
