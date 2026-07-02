package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// IFRS 15 revenue-recognition endpoints.

type createScheduleRequest struct {
	SourceRef   string `json:"sourceRef" binding:"required"`
	Total       string `json:"total" binding:"required"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`      // ratable | milestone
	StartPeriod string `json:"startPeriod"` // YYYY-MM
	Periods     int    `json:"periods"`
	Obligations []struct {
		Description string `json:"description"`
		Amount      string `json:"amount"`
	} `json:"obligations"`
}

type recognitionRunRequest struct {
	Period string `json:"period" binding:"required"` // YYYY-MM
}

type accrueRequest struct {
	Ref    string `json:"ref" binding:"required"`
	Amount string `json:"amount" binding:"required"`
}

// CreateRevenueSchedule defers revenue and schedules its recognition.
func (a *API) CreateRevenueSchedule(c *gin.Context) {
	var req createScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	total, err := decimal.NewFromString(req.Total)
	if err != nil || total.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid total")
		return
	}
	in := repository.CreateScheduleInput{
		SourceRef:   strings.TrimSpace(req.SourceRef),
		Total:       total,
		Currency:    req.Currency,
		Method:      req.Method,
		StartPeriod: strings.TrimSpace(req.StartPeriod),
		Periods:     req.Periods,
	}
	for _, o := range req.Obligations {
		amt, err := decimal.NewFromString(o.Amount)
		if err != nil || amt.LessThanOrEqual(decimal.Zero) {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid obligation amount")
			return
		}
		in.Obligations = append(in.Obligations, repository.ObligationInput{Description: o.Description, Amount: amt})
	}
	sched, err := a.Ledger.CreateRevenueSchedule(c.Request.Context(), in, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, revrecErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, sched)
}

// RunRevenueRecognition releases due ratable slices for a period.
func (a *API) RunRevenueRecognition(c *gin.Context) {
	var req recognitionRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Ledger.RunRevenueRecognition(c.Request.Context(), strings.TrimSpace(req.Period), chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, revrecErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// SatisfyObligation recognises one milestone obligation.
func (a *API) SatisfyObligation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid obligation id")
		return
	}
	entry, err := a.Ledger.SatisfyObligation(c.Request.Context(), id, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, revrecErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// AccrueRevenue recognises revenue earned ahead of billing.
func (a *API) AccrueRevenue(c *gin.Context) {
	var req accrueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid amount")
		return
	}
	entry, err := a.Ledger.AccrueRevenue(c.Request.Context(), strings.TrimSpace(req.Ref), amount, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, revrecErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// ListRevenueSchedules returns recent schedules.
func (a *API) ListRevenueSchedules(c *gin.Context) {
	limit, _ := pagination(c)
	items, err := a.Ledger.ListRevenueSchedules(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list schedules")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func revrecErrStatus(err error) int {
	switch {
	case errors.Is(err, ledger.ErrPeriodClosed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, repository.ErrScheduleExists):
		return http.StatusConflict
	case errors.Is(err, repository.ErrObligationSatisfied):
		return http.StatusConflict
	case errors.Is(err, repository.ErrScheduleNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
