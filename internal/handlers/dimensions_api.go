package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/alvor-technologies/iag-platform-go/apierr"
)

type createDimensionRequest struct {
	Code string `json:"code" binding:"required"`
	Name string `json:"name" binding:"required"`
}

func (a *API) ListProjects(c *gin.Context) {
	items, err := a.Ledger.ListProjects(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list projects")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateProject(c *gin.Context) {
	var req createDimensionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.Ledger.CreateProject(c.Request.Context(), strings.ToUpper(req.Code), req.Name)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create project")
		return
	}
	c.JSON(http.StatusCreated, d)
}

func (a *API) ListCostCenters(c *gin.Context) {
	items, err := a.Ledger.ListCostCenters(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list cost centers")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateCostCenter(c *gin.Context) {
	var req createDimensionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.Ledger.CreateCostCenter(c.Request.Context(), strings.ToUpper(req.Code), req.Name)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create cost center")
		return
	}
	c.JSON(http.StatusCreated, d)
}

// DeactivateCostCenter soft-archives a cost-centre (active=false) so it stops
// appearing in pickers while its historical postings remain intact.
func (a *API) DeactivateCostCenter(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid cost center id")
		return
	}
	if err := a.Ledger.DeactivateCostCenter(c.Request.Context(), id); err != nil {
		apierr.JSONStatus(c, http.StatusNotFound, "cost center not found")
		return
	}
	c.Status(http.StatusNoContent)
}

// ProjectPL reports revenue/expense for one project (?from=&to=).
func (a *API) ProjectPL(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid project id")
		return
	}
	rows, err := a.Ledger.ProjectPL(c.Request.Context(), id, dateParam(c, "from"), dateParam(c, "to"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build project P&L")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}
