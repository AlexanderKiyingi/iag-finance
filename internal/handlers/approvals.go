package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/middleware"
	"github.com/iag-finance/backend/internal/repository"
)

func (a *API) ListApprovalTiers(c *gin.Context) {
	tiers, err := a.Ledger.ListApprovalTiers(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list approval tiers")
		return
	}
	c.JSON(http.StatusOK, gin.H{"tiers": tiers})
}

func (a *API) ListApprovals(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	items, err := a.Ledger.ListApprovals(c.Request.Context(), strings.TrimSpace(c.Query("status")), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list approvals")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type submitApprovalBody struct {
	TargetType string `json:"targetType"` // journal | payment
	EntryID    string `json:"entryId"`    // journal
	Direction  string `json:"direction"`  // payment: ar | ap
	OpenItemID string `json:"openItemId"` // payment
	Amount     string `json:"amount"`     // payment (decimal string)
	Currency   string `json:"currency"`   // payment
	PaymentRef string `json:"paymentRef"` // payment
}

// SubmitApproval opens a tiered approval for a high-value journal or payment. If
// the amount is below the first band it returns approvalRequired=false and the
// caller may proceed with the direct endpoint.
func (a *API) SubmitApproval(c *gin.Context) {
	var body submitApprovalBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid request body")
		return
	}
	actor := chainActor(c)
	ctx := c.Request.Context()

	switch body.TargetType {
	case "journal":
		entryID, err := uuid.Parse(strings.TrimSpace(body.EntryID))
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "entryId must be a uuid")
			return
		}
		entry, err := a.Ledger.GetJournalEntry(ctx, entryID)
		if err != nil {
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not load entry")
			return
		}
		if entry == nil {
			apierr.JSONStatus(c, http.StatusNotFound, "journal entry not found")
			return
		}
		amount := decimal.Zero
		for _, l := range entry.Lines {
			amount = amount.Add(l.Debit)
		}
		ap, required, err := a.Ledger.SubmitForApproval(ctx, "journal", amount, "",
			map[string]any{"entryId": entryID.String()}, actor, entry.Description)
		a.respondSubmit(c, ap, required, err)
	case "payment":
		dir := strings.ToLower(strings.TrimSpace(body.Direction))
		if dir != "ar" && dir != "ap" {
			apierr.JSONStatus(c, http.StatusBadRequest, "direction must be ar or ap")
			return
		}
		itemID, err := uuid.Parse(strings.TrimSpace(body.OpenItemID))
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "openItemId must be a uuid")
			return
		}
		amount, err := decimal.NewFromString(strings.TrimSpace(body.Amount))
		if err != nil || amount.LessThanOrEqual(decimal.Zero) {
			apierr.JSONStatus(c, http.StatusBadRequest, "amount must be a positive decimal")
			return
		}
		ap, required, err := a.Ledger.SubmitForApproval(ctx, "payment", amount, strings.TrimSpace(body.Currency),
			map[string]any{"direction": dir, "openItemId": itemID.String(), "paymentRef": strings.TrimSpace(body.PaymentRef)},
			actor, dir+" payment "+strings.TrimSpace(body.PaymentRef))
		a.respondSubmit(c, ap, required, err)
	default:
		apierr.JSONStatus(c, http.StatusBadRequest, "targetType must be journal or payment")
	}
}

func (a *API) respondSubmit(c *gin.Context, ap *repository.Approval, required bool, err error) {
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not submit approval")
		return
	}
	if !required {
		c.JSON(http.StatusOK, gin.H{"approvalRequired": false})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"approvalRequired": true, "approval": ap})
}

// ApprovalGuard returns true (and writes a 409) when tiered approval is enforced
// and the amount reaches an approval band — the caller must route through
// POST /v1/approvals instead of the direct post/payment endpoint. Returns false
// when approval is off or the amount is below the first band.
func (a *API) ApprovalGuard(c *gin.Context, amount decimal.Decimal) bool {
	if !a.Cfg.RequireApproval {
		return false
	}
	required, err := a.Ledger.ApprovalRequired(c.Request.Context(), amount)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "approval check failed")
		return true
	}
	if required {
		apierr.JSON(c, http.StatusConflict, "APPROVAL_REQUIRED",
			"amount requires tiered approval; submit via POST /v1/approvals")
		return true
	}
	return false
}

func (a *API) ApproveApproval(c *gin.Context) { a.decideApproval(c, true) }
func (a *API) RejectApproval(c *gin.Context)  { a.decideApproval(c, false) }

func (a *API) decideApproval(c *gin.Context, approve bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid approval id")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)
	hasPerm := func(code string) bool { return middleware.HasPerm(c, code) }
	actor := chainActor(c)
	ctx := c.Request.Context()

	var ap *repository.Approval
	var prog *repository.ApprovalProgress
	if approve {
		ap, prog, err = a.Ledger.ApproveApproval(ctx, id, actor, hasPerm, strings.TrimSpace(body.Note))
	} else {
		ap, prog, err = a.Ledger.RejectApproval(ctx, id, actor, hasPerm, strings.TrimSpace(body.Note))
	}
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrApprovalNotFound):
			apierr.JSONStatus(c, http.StatusNotFound, "approval not found")
		case errors.Is(err, repository.ErrApprovalForbidden):
			apierr.JSONStatus(c, http.StatusForbidden, err.Error())
		case errors.Is(err, repository.ErrApprovalConflict):
			apierr.JSONStatus(c, http.StatusConflict, err.Error())
		case errors.Is(err, ledger.ErrPeriodClosed):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "accounting period is closed")
		default:
			apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": ap, "progress": prog})
}
