package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/google/uuid"
)

// PortalAP returns AP open items scoped to the caller's party_id query param.
// Portal users pass party_id from their linked supplier/vendor profile.
func (a *API) PortalAP(c *gin.Context) {
	partyRaw := c.Query("party_id")
	if partyRaw == "" {
		apierr.BadRequest(c, "party_id query param required")
		return
	}
	partyID, err := uuid.Parse(partyRaw)
	if err != nil {
		apierr.BadRequest(c, "invalid party_id")
		return
	}
	items, err := a.Ledger.ListAPByPartyID(c.Request.Context(), partyID, 50, 0)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list AP items")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "ap_open_items", "party_id": partyID.String()})
}
