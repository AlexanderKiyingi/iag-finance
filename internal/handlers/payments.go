package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

type applyPaymentRequest struct {
	Amount      string `json:"amount" binding:"required"`
	Currency    string `json:"currency"`
	PaymentRef  string `json:"paymentRef"`
	NotifyEmail string `json:"notifyEmail"`
}

func (a *API) ApplyARPayment(c *gin.Context) {
	a.applyPayment(c, "ar")
}

func (a *API) ApplyAPPayment(c *gin.Context) {
	a.applyPayment(c, "ap")
}

func (a *API) applyPayment(c *gin.Context, direction string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req applyPaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := ledger.ParsePaymentAmount(req.Amount)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	paymentRef := req.PaymentRef
	if paymentRef == "" {
		paymentRef = uuid.NewString()
	}

	var payment interface{}
	var item interface{}
	switch direction {
	case "ar":
		p, ar, err := a.Ledger.ApplyARPayment(c.Request.Context(), id, amount, req.Currency, paymentRef)
		if mapPaymentErr(c, err) {
			return
		}
		payment, item = p, ar
		if req.NotifyEmail != "" && ar != nil && ar.Status == "closed" && a.Events != nil {
			a.Events.PublishNotification(c.Request.Context(), req.NotifyEmail, "invoice-ready-email", map[string]string{
				"documentRef": ar.DocumentRef,
				"amount":      ar.Amount,
				"currency":    ar.Currency,
			})
		}
		logBusinessEvent(c, a.Audit, auditlog.EventARPayment, "ar_open_item", id.String(), http.StatusCreated, map[string]any{
			"amount": req.Amount, "paymentRef": paymentRef,
		})
	case "ap":
		p, ap, err := a.Ledger.ApplyAPPayment(c.Request.Context(), id, amount, req.Currency, paymentRef)
		if mapPaymentErr(c, err) {
			return
		}
		payment, item = p, ap
		logBusinessEvent(c, a.Audit, auditlog.EventAPPayment, "ap_open_item", id.String(), http.StatusCreated, map[string]any{
			"amount": req.Amount, "paymentRef": paymentRef,
		})
	}

	c.JSON(http.StatusCreated, gin.H{"payment": payment, "item": item})
}

func (a *API) ListARPayments(c *gin.Context) {
	a.listPayments(c)
}

func (a *API) ListAPPayments(c *gin.Context) {
	a.listPayments(c)
}

func (a *API) listPayments(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := a.Ledger.ListPaymentsForItem(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list payments")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func mapPaymentErr(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ledger.ErrOpenItemNotFound):
		apierr.JSONStatus(c, http.StatusNotFound, "open item not found")
	case errors.Is(err, ledger.ErrPaymentExceeds):
		apierr.JSONStatus(c, http.StatusUnprocessableEntity, "payment exceeds open balance")
	default:
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not apply payment")
	}
	return true
}
