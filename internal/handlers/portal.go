package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
)

// PortalMe returns the linked party profile for portal users.
func (a *API) PortalMe(c *gin.Context) {
	userID, ok := portalUserID(c)
	if !ok {
		apierr.Unauthorized(c, "authentication required")
		return
	}
	partyID, err := a.Ledger.PartyIDForPlatformUser(c.Request.Context(), userID)
	if err != nil {
		if err == repository.ErrPortalPartyNotLinked {
			apierr.WriteWith(c, http.StatusForbidden, apierr.CodeForbidden, "portal profile not linked", nil)
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve portal profile")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"party_id": partyID.String(),
		"source":   "portal_party_links",
	})
}

// PortalAP returns AP open items for the authenticated portal user's linked party only.
func (a *API) PortalAP(c *gin.Context) {
	userID, ok := portalUserID(c)
	if !ok {
		apierr.Unauthorized(c, "authentication required")
		return
	}
	partyID, err := a.Ledger.PartyIDForPlatformUser(c.Request.Context(), userID)
	if err != nil {
		if err == repository.ErrPortalPartyNotLinked {
			apierr.WriteWith(c, http.StatusForbidden, apierr.CodeForbidden, "portal profile not linked", nil)
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve portal party")
		return
	}
	items, err := a.Ledger.ListAPByPartyID(c.Request.Context(), partyID, 50, 0)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list AP items")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "ap_open_items", "party_id": partyID.String()})
}

func portalUserID(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get("userID")
	if !ok {
		return uuid.Nil, false
	}
	switch id := v.(type) {
	case uuid.UUID:
		return id, id != uuid.Nil
	case string:
		uid, err := uuid.Parse(id)
		return uid, err == nil
	default:
		return uuid.Nil, false
	}
}
