package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/ledger"
)

func (a *API) ListPayrollEmployees(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "payroll mirror unavailable")
		return
	}
	limit := payrollQueryLimit(c, 100)
	items, err := a.Repo.ListPayrollEmployeeRefs(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list payroll employees")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "iag-erp-events"})
}

func (a *API) ListPayrollLeaveAccruals(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "payroll mirror unavailable")
		return
	}
	limit := payrollQueryLimit(c, 100)
	items, err := a.Repo.ListPayrollLeaveAccruals(c.Request.Context(), c.Query("employee_no"), c.Query("status"), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list leave accruals")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "iag-erp-events"})
}

type postPayrollRunRequest struct {
	RunRef          string `json:"runRef" binding:"required"`
	Period          string `json:"period" binding:"required"`
	Gross           string `json:"gross" binding:"required"`
	PAYE            string `json:"paye"`
	NSSF            string `json:"nssf"`
	OtherDeductions string `json:"otherDeductions"`
	Net             string `json:"net" binding:"required"`
	Currency        string `json:"currency"`
}

// PostPayrollRun books a finalized payroll run to the general ledger
// (Dr salary expense, Cr statutory payables + net pay). Idempotent on runRef.
func (a *API) PostPayrollRun(c *gin.Context) {
	var req postPayrollRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if !periodRE.MatchString(req.Period) {
		apierr.JSONStatus(c, http.StatusBadRequest, "period must be in YYYY-MM format")
		return
	}
	amounts, err := parsePayrollAmounts(req)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	run, err := a.Ledger.PostPayrollRun(c.Request.Context(), amounts)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ledger.ErrPayrollUnbalanced), errors.Is(err, ledger.ErrPeriodClosed),
			errors.Is(err, ledger.ErrAccountNotFound):
			status = http.StatusUnprocessableEntity
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, run)
	logBusinessEvent(c, a.Audit, auditlog.EventPayrollRunPosted, "payroll_run", run.RunRef, http.StatusCreated, map[string]any{
		"period": run.Period, "gross": run.Gross, "net": run.Net, "journalEntryId": run.JournalEntryID,
	})
}

// ListPayrollRuns returns posted payroll runs, newest first.
func (a *API) ListPayrollRuns(c *gin.Context) {
	items, err := a.Ledger.ListPayrollRuns(c.Request.Context(), payrollQueryLimit(c, 100))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list payroll runs")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func parsePayrollAmounts(req postPayrollRunRequest) (ledger.PayrollRunInput, error) {
	gross, err := decimal.NewFromString(req.Gross)
	if err != nil {
		return ledger.PayrollRunInput{}, errors.New("invalid gross amount")
	}
	net, err := decimal.NewFromString(req.Net)
	if err != nil {
		return ledger.PayrollRunInput{}, errors.New("invalid net amount")
	}
	paye := optionalDecimal(req.PAYE)
	nssf := optionalDecimal(req.NSSF)
	other := optionalDecimal(req.OtherDeductions)
	return ledger.PayrollRunInput{
		RunRef:          req.RunRef,
		Period:          req.Period,
		Gross:           gross,
		PAYE:            paye,
		NSSF:            nssf,
		OtherDeductions: other,
		Net:             net,
		Currency:        req.Currency,
	}, nil
}

func optionalDecimal(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func payrollQueryLimit(c *gin.Context, def int) int {
	raw := c.DefaultQuery("limit", "")
	if raw == "" {
		return def
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return def
	}
	if limit > 500 {
		return 500
	}
	return limit
}
