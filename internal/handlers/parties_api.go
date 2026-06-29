package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/alvor-technologies/iag-platform-go/apierr"
)

// createPartyRequest is the body for POST /v1/customers and /v1/vendors. Only
// code and name are required; the rest are optional billing-contact details the
// frontend create-new dialog may collect.
type createPartyRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Currency string `json:"currency"`
}

func (a *API) ListCustomers(c *gin.Context) {
	items, err := a.Ledger.ListCustomers(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list customers")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateCustomer(c *gin.Context) {
	var req createPartyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.Ledger.CreateCustomer(c.Request.Context(),
		strings.ToUpper(strings.TrimSpace(req.Code)), strings.TrimSpace(req.Name),
		strings.TrimSpace(req.Email), strings.TrimSpace(req.Phone), strings.ToUpper(strings.TrimSpace(req.Currency)))
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create customer")
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (a *API) ListVendors(c *gin.Context) {
	items, err := a.Ledger.ListVendors(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list vendors")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) CreateVendor(c *gin.Context) {
	var req createPartyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.Ledger.CreateVendor(c.Request.Context(),
		strings.ToUpper(strings.TrimSpace(req.Code)), strings.TrimSpace(req.Name),
		strings.TrimSpace(req.Email), strings.TrimSpace(req.Phone), strings.ToUpper(strings.TrimSpace(req.Currency)))
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create vendor")
		return
	}
	c.JSON(http.StatusCreated, p)
}
