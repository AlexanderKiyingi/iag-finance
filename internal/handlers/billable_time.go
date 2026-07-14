package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/repository"
)

// ListTimeEntries backs GET /billing/time-entries.
func (a *API) ListTimeEntries(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListTimeEntries(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list time entries")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func decOrZero(s string) decimal.Decimal {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decimal.Zero
	}
	return d
}

// RecordTimeEntry backs POST /billing/time-entries.
func (a *API) RecordTimeEntry(c *gin.Context) {
	var body struct {
		EntryRef string `json:"entryRef"`
		Customer string `json:"customer"`
		Employee string `json:"employee"`
		Project  string `json:"project"`
		Hours    string `json:"hours"`
		Rate     string `json:"rate"`
		Amount   string `json:"amount"`
		WorkDate string `json:"workDate"`
		Currency string `json:"currency"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	workDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.WorkDate))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "workDate must be YYYY-MM-DD")
		return
	}
	hours := decOrZero(body.Hours)
	rate := decOrZero(body.Rate)
	amount := decOrZero(body.Amount)
	if amount.LessThanOrEqual(decimal.Zero) {
		// Derive from hours × rate when the amount isn't supplied.
		amount = hours.Mul(rate)
	}
	entry, err := a.Ledger.RecordTimeEntry(c.Request.Context(), repository.CreateTimeEntryInput{
		EntryRef: strings.TrimSpace(body.EntryRef), Customer: strings.TrimSpace(body.Customer),
		Employee: strings.TrimSpace(body.Employee), Project: strings.TrimSpace(body.Project),
		Hours: hours, Rate: rate, Amount: amount, WorkDate: workDate.UTC(),
		Currency: strings.TrimSpace(body.Currency), Notes: strings.TrimSpace(body.Notes),
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			apierr.JSONStatus(c, http.StatusConflict, "a time entry with this reference already exists")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not record time entry")
		return
	}
	c.JSON(http.StatusCreated, entry)
}
