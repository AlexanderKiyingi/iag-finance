package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/ledger"
)

// CloseFiscalYear posts the year-end closing entry (revenue/expense → retained
// earnings) and locks the year's twelve periods. Idempotent.
func (a *API) CloseFiscalYear(c *gin.Context) {
	year, err := strconv.Atoi(c.Param("year"))
	if err != nil || year < 2000 || year > 9999 {
		apierr.JSONStatus(c, http.StatusBadRequest, "year must be a four-digit calendar year")
		return
	}

	var by *uuid.UUID
	if raw, ok := c.Get("userID"); ok {
		if id, ok := raw.(uuid.UUID); ok {
			by = &id
		}
	}

	entry, err := a.Ledger.CloseFiscalYear(c.Request.Context(), year, chainActor(c), by)
	if err != nil {
		switch {
		case errors.Is(err, ledger.ErrYearHasDrafts):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ledger.ErrNothingToClose):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, err.Error())
		default:
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not close fiscal year")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"year": year, "status": "closed", "closingEntry": entry})
	logBusinessEvent(c, a.Audit, auditlog.EventFiscalPeriodClosed, "fiscal_year", strconv.Itoa(year), http.StatusOK, map[string]any{
		"year": year, "closingEntryId": entry.ID.String(),
	})
}
