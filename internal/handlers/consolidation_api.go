package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/repository"
)

// ConsolidationEliminations returns the intra-group + investment/equity
// eliminations for the consolidated scope as of a date. Requires the request to
// be consolidated (?consolidated=true, gated by finance.view_consolidated); a
// single-entity scope has nothing to eliminate.
func (a *API) ConsolidationEliminations(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	if len(scope) < 2 {
		c.JSON(http.StatusOK, repository.ConsolidationSummary{
			Transactional: []repository.EliminationRow{},
			Structural:    []repository.EliminationRow{},
			NCI:           "0.00",
			Goodwill:      "0.00",
		})
		return
	}
	asOf := dateParam(c, "asOf")
	if asOf == nil {
		asOf = dateParam(c, "to")
	}
	summary, err := a.Ledger.ConsolidationEliminations(c.Request.Context(), asOf, scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build eliminations")
		return
	}
	c.JSON(http.StatusOK, summary)
}
