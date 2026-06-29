package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// ListBanks serves the bank reference list (GET /v1/banks) that backs the
// frontend "Bank Name" dropdown — licensed banks, mobile-money wallets, and
// petty cash. Read-only; the list is seeded by migration 039.
func (a *API) ListBanks(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "bank reference list unavailable")
		return
	}
	items, err := a.Repo.ListBanks(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list banks")
		return
	}
	c.JSON(http.StatusOK, gin.H{"banks": items})
}
