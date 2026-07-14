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

// ListFXConversions backs GET /fx/conversions.
func (a *API) ListFXConversions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListFXConversions(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list FX conversions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RecordFXConversion backs POST /fx/conversions.
func (a *API) RecordFXConversion(c *gin.Context) {
	var body struct {
		ConversionRef   string `json:"conversionRef"`
		FromAccount     string `json:"fromAccount"`
		FromCurrency    string `json:"fromCurrency"`
		FromAmount      string `json:"fromAmount"`
		ToAccount       string `json:"toAccount"`
		ToCurrency      string `json:"toCurrency"`
		ExchangeRate    string `json:"exchangeRate"`
		ConvertedAmount string `json:"convertedAmount"`
		Fees            string `json:"fees"`
		GainLossAccount string `json:"gainLossAccount"`
		ConversionDate  string `json:"conversionDate"`
		Notes           string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	fromAmount := decOrZero(body.FromAmount)
	if fromAmount.LessThanOrEqual(decimal.Zero) {
		apierr.JSONStatus(c, http.StatusBadRequest, "fromAmount must be a positive source amount")
		return
	}
	convDate, err := time.Parse("2006-01-02", strings.TrimSpace(body.ConversionDate))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "conversionDate must be YYYY-MM-DD")
		return
	}
	rec, err := a.Ledger.RecordFXConversion(c.Request.Context(), repository.CreateFXConversionInput{
		ConversionRef: strings.TrimSpace(body.ConversionRef), FromAccount: strings.TrimSpace(body.FromAccount),
		FromCurrency: strings.TrimSpace(body.FromCurrency), FromAmount: fromAmount,
		ToAccount: strings.TrimSpace(body.ToAccount), ToCurrency: strings.TrimSpace(body.ToCurrency),
		ExchangeRate: decOrZero(body.ExchangeRate), ConvertedAmount: decOrZero(body.ConvertedAmount),
		Fees: decOrZero(body.Fees), GainLossAccount: strings.TrimSpace(body.GainLossAccount),
		ConversionDate: convDate.UTC(), Notes: strings.TrimSpace(body.Notes),
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			apierr.JSONStatus(c, http.StatusConflict, "a conversion with this reference already exists")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not record FX conversion")
		return
	}
	c.JSON(http.StatusCreated, rec)
}
