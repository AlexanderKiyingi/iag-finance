package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/alvor-technologies/iag-platform-go/apierr"
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
