package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

// ListIntangibleAssets backs GET /intangible-assets.
func (a *API) ListIntangibleAssets(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListIntangibleAssets(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list intangible assets")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RegisterIntangibleAsset backs POST /intangible-assets (IAS 38). Capitalizes the
// cost into 1700 Intangible Assets unless recordOnly is set.
func (a *API) RegisterIntangibleAsset(c *gin.Context) {
	var body struct {
		AssetRef         string `json:"assetRef"`
		Description      string `json:"description"`
		Category         string `json:"category"`
		Cost             string `json:"cost"`
		InServiceDate    string `json:"inServiceDate"`
		UsefulLifeMonths int    `json:"usefulLifeMonths"`
		Currency         string `json:"currency"`
		// RecordOnly skips the capitalization reclass (default: capitalize).
		// CapitalizeFromAccount is the expense account the cost is reclassed out
		// of (default 5000 — where procurement books capital purchases).
		RecordOnly            bool   `json:"recordOnly"`
		CapitalizeFromAccount string `json:"capitalizeFromAccount"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.AssetRef) == "" || body.UsefulLifeMonths <= 0 {
		apierr.JSONStatus(c, http.StatusBadRequest, "assetRef and a positive usefulLifeMonths are required")
		return
	}
	cost, err := decimal.NewFromString(strings.TrimSpace(body.Cost))
	if err != nil || cost.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "cost must be a positive amount")
		return
	}
	inService, err := time.Parse("2006-01-02", strings.TrimSpace(body.InServiceDate))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "inServiceDate must be YYYY-MM-DD")
		return
	}
	capitalizeFrom := ""
	if !body.RecordOnly {
		capitalizeFrom = strings.TrimSpace(body.CapitalizeFromAccount)
		if capitalizeFrom == "" {
			capitalizeFrom = "5000"
		}
	}
	asset, err := a.Ledger.RegisterIntangibleAsset(c.Request.Context(), repository.CreateIntangibleAssetInput{
		AssetRef: strings.TrimSpace(body.AssetRef), Description: body.Description, Category: body.Category,
		Cost: cost, InServiceDate: inService.UTC(), UsefulLifeMonths: body.UsefulLifeMonths,
		Currency: strings.TrimSpace(body.Currency), CapitalizeFromAccount: capitalizeFrom,
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			apierr.JSONStatus(c, http.StatusConflict, "asset already registered")
			return
		}
		if errors.Is(err, ledger.ErrPeriodClosed) {
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "in-service period is closed")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not register intangible asset")
		return
	}
	c.JSON(http.StatusCreated, asset)
}
