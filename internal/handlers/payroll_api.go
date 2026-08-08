package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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

type postLeaveProvisionRequest struct {
	ProvisionRef string `json:"provisionRef" binding:"required"`
	Period       string `json:"period" binding:"required"`
	// Amount is the signed movement in the liability, not a closing balance.
	Amount   string `json:"amount" binding:"required"`
	Currency string `json:"currency"`
	Note     string `json:"note"`
}

// PostLeaveProvision books a movement in the accrued-leave liability.
//
// The figure is stated by whoever calculates payroll, the same way payroll
// totals are: HR reports leave taken but never the balance earned, and no
// service holds a pay rate, so the platform cannot compute it yet. provisionRef
// makes a restated period idempotent rather than cumulative.
func (a *API) PostLeaveProvision(c *gin.Context) {
	var req postLeaveProvisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if !periodRE.MatchString(req.Period) {
		apierr.JSONStatus(c, http.StatusBadRequest, "period must be in YYYY-MM format")
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "amount must be a decimal number")
		return
	}

	prov, err := a.Ledger.PostLeaveProvision(c.Request.Context(), ledger.LeaveProvisionInput{
		ProvisionRef: strings.TrimSpace(req.ProvisionRef),
		Period:       req.Period,
		Amount:       amount,
		Currency:     strings.TrimSpace(req.Currency),
		Note:         strings.TrimSpace(req.Note),
	})
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ledger.ErrLeaveProvisionZero), errors.Is(err, ledger.ErrPeriodClosed),
			errors.Is(err, ledger.ErrAccountNotFound):
			status = http.StatusUnprocessableEntity
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, prov)
	logBusinessEvent(c, a.Audit, auditlog.EventPayrollRunPosted, "leave_provision", prov.ProvisionRef,
		http.StatusCreated, map[string]any{
			"period": prov.Period, "amount": prov.Amount, "journalEntryId": prov.JournalEntryID,
		})
}

// ListLeaveProvisions returns booked provisions, newest first.
func (a *API) ListLeaveProvisions(c *gin.Context) {
	items, err := a.Ledger.ListLeaveProvisions(c.Request.Context(),
		strings.TrimSpace(c.Query("period")), payrollQueryLimit(c, 100))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list leave provisions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type valueLeaveRequest struct {
	// Year defaults to the year of ValuedAt.
	Year int `json:"year"`
	// ValuedAt defaults to today. One valuation per date: re-running a day
	// replaces it rather than stacking movements on the same closing figure.
	ValuedAt string `json:"valuedAt"`
}

// ValueLeaveLiability measures the accrued-leave obligation from the balances
// and rates HR reports, and books the movement since the last valuation.
//
// This is the computed counterpart to POST /payroll/leave-provisions, which
// remains for correction entries and for deployments where HR does not yet
// publish balances or rates.
func (a *API) ValueLeaveLiability(c *gin.Context) {
	var req valueLeaveRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	valuedAt := time.Now().UTC()
	if s := strings.TrimSpace(req.ValuedAt); s != "" {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "valuedAt must be YYYY-MM-DD")
			return
		}
		valuedAt = parsed
	}

	out, err := a.Ledger.ValueLeaveLiability(c.Request.Context(), req.Year, valuedAt)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ledger.ErrPeriodClosed) || errors.Is(err, ledger.ErrAccountNotFound) {
			status = http.StatusUnprocessableEntity
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusOK, out)
	logBusinessEvent(c, a.Audit, auditlog.EventPayrollRunPosted, "leave_valuation",
		out.ValuedAt.Format("2006-01-02"), http.StatusOK, map[string]any{
			"totalLiability": out.TotalLiability.String(),
			"movement":       out.Movement.String(),
			"unrated":        out.EmployeesUnrated,
		})
}

// ListLeaveBalances shows the days HR reports as owed, joined to the rate each
// would be valued at. A balance with no rate appears with a null value rather
// than being hidden — it is an obligation nobody can measure, and this is where
// that should be visible.
func (a *API) ListLeaveBalances(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "payroll mirror unavailable")
		return
	}
	year, _ := strconv.Atoi(strings.TrimSpace(c.Query("year")))
	items, err := a.Repo.ListLeaveBalances(c.Request.Context(), year, payrollQueryLimit(c, 200))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list leave balances")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "source": "iag-erp-events"})
}

// ListLeaveValuations shows what the liability was measured at and when.
func (a *API) ListLeaveValuations(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "payroll mirror unavailable")
		return
	}
	items, err := a.Repo.ListLeaveValuations(c.Request.Context(), payrollQueryLimit(c, 100))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list leave valuations")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListOrphanedAPJournals reports journals that credit the payables control
// account with nothing in the AP subledger against them.
//
// A payable in that state is invisible: absent from aged payables, with no
// vendor to pay, and leaving an unexplained difference on the control account.
// Every fleet fuel record booked before finance created the payable is in it.
//
// This reports rather than repairs. The vendor is not recoverable from a
// journal — it lives in the system that raised the cost — so an automated fix
// would produce payables that balance the control account and still cannot be
// paid to anyone.
func (a *API) ListOrphanedAPJournals(c *gin.Context) {
	if a.Repo == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "repository unavailable")
		return
	}
	items, summary, err := a.Repo.ListOrphanedAPJournals(c.Request.Context(), payrollQueryLimit(c, 200))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list orphaned AP journals")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "summary": summary})
}
