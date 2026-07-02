package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/integrations"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/iag-finance/backend/internal/usersclient"
)

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type API struct {
	Ledger          *ledger.Service
	Audit           *auditlog.Service
	DB              HealthChecker
	ConsumerEnabled bool
	Events          *events.Bus
	Integrations    *integrations.Registry
	Cfg             config.Config
	Users           *usersclient.Client
	Repo            *repository.Repository
}

func (a *API) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"service":   "finance",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *API) Ready(c *gin.Context) {
	if a.DB != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := a.DB.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "not_ready",
				"service": "finance",
				"error":   "database unavailable",
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "service": "finance"})
}

func (a *API) FinanceHealth(c *gin.Context) {
	items := integrations.FinanceHealth()
	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"integrations": items,
	})
}

func (a *API) ListChartOfAccounts(c *gin.Context) {
	items, err := a.Ledger.ListChartOfAccounts(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list accounts")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type createAccountRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	AccountType string `json:"accountType" binding:"required,oneof=asset liability equity revenue expense"`
	Currency    string `json:"currency"`
	ParentID    string `json:"parentId"`
}

func (a *API) CreateChartAccount(c *gin.Context) {
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "UGX"
	}
	var parentID *uuid.UUID
	if req.ParentID != "" {
		id, err := uuid.Parse(req.ParentID)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid parentId")
			return
		}
		parentID = &id
	}
	acct, err := a.Ledger.CreateChartAccount(c.Request.Context(), req.Code, req.Name, req.AccountType, currency, parentID)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create account")
		return
	}
	c.JSON(http.StatusCreated, acct)
	logBusinessEvent(c, a.Audit, auditlog.EventChartAccountCreated, "chart_of_account", acct.ID.String(), http.StatusCreated, map[string]any{
		"code": acct.Code, "name": acct.Name, "accountType": acct.AccountType,
	})
}

var validAccountTypes = map[string]bool{
	"asset": true, "liability": true, "equity": true, "revenue": true, "expense": true,
}

type updateAccountRequest struct {
	Name                  *string `json:"name"`
	AccountType           *string `json:"accountType"`
	Currency              *string `json:"currency"`
	ParentID              *string `json:"parentId"`
	Active                *bool   `json:"active"`
	RestrictToNaturalSide *bool   `json:"restrictToNaturalSide"`
}

func (a *API) UpdateChartAccount(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid account id")
		return
	}
	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.AccountType != nil && !validAccountTypes[*req.AccountType] {
		apierr.JSONStatus(c, http.StatusBadRequest, "accountType must be one of asset, liability, equity, revenue, expense")
		return
	}
	// Code is immutable; map an empty name to "no change" so a blank field in a
	// generic edit form doesn't wipe the account name.
	if req.Name != nil && *req.Name == "" {
		req.Name = nil
	}
	var parentID *uuid.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid parentId")
			return
		}
		if pid == id {
			apierr.JSONStatus(c, http.StatusBadRequest, "an account cannot be its own parent")
			return
		}
		parentID = &pid
	}

	acct, err := a.Ledger.UpdateChartAccount(c.Request.Context(), id, req.Name, req.AccountType, req.Currency, parentID, req.Active, req.RestrictToNaturalSide)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not update account")
		return
	}
	if acct == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "account not found")
		return
	}
	c.JSON(http.StatusOK, acct)
	logBusinessEvent(c, a.Audit, auditlog.EventChartAccountUpdated, "chart_of_account", acct.ID.String(), http.StatusOK, map[string]any{
		"code": acct.Code, "name": acct.Name, "accountType": acct.AccountType, "active": acct.Active,
	})
}

func (a *API) DeleteChartAccount(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid account id")
		return
	}
	deactivated, err := a.Ledger.DeactivateChartAccount(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not deactivate account")
		return
	}
	if !deactivated {
		apierr.JSONStatus(c, http.StatusNotFound, "account not found")
		return
	}
	c.Status(http.StatusNoContent)
	logBusinessEvent(c, a.Audit, auditlog.EventChartAccountDeactivated, "chart_of_account", id.String(), http.StatusNoContent, nil)
}

func (a *API) ListJournalEntries(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListJournalEntries(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list entries")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"_note": "General ledger — operational events are booked via the finance bus consumer",
	})
}

func (a *API) GetJournalEntry(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := a.Ledger.GetJournalEntry(c.Request.Context(), id)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not load entry")
		return
	}
	if entry == nil {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	c.JSON(http.StatusOK, entry)
}

type journalLineRequest struct {
	AccountCode  string `json:"accountCode" binding:"required"`
	Debit        string `json:"debit"`
	Credit       string `json:"credit"`
	Memo         string `json:"memo"`
	CostCenterID string `json:"costCenterId"`
	ProjectID    string `json:"projectId"`
}

type createJournalRequest struct {
	Description string               `json:"description" binding:"required"`
	Lines       []journalLineRequest `json:"lines" binding:"required,min=2"`
}

func (a *API) CreateJournalEntry(c *gin.Context) {
	var req createJournalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	lines, err := parseLines(req.Lines)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	var createdBy *uuid.UUID
	if raw, ok := c.Get("userID"); ok {
		id := raw.(uuid.UUID)
		createdBy = &id
	}
	entry, err := a.Ledger.CreateJournalEntry(c.Request.Context(), ledger.CreateEntryInput{
		Description: req.Description,
		Lines:       lines,
		CreatedBy:   createdBy,
	})
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ledger.ErrUnbalancedEntry), errors.Is(err, ledger.ErrEmptyEntry):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, ledger.ErrAccountNotFound):
			status = http.StatusBadRequest
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entry)
	logBusinessEvent(c, a.Audit, auditlog.EventJournalCreated, "journal_entry", entry.ID.String(), http.StatusCreated, map[string]any{
		"entryNumber": entry.EntryNumber, "status": entry.Status,
	})
}

func (a *API) PostJournalEntry(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	// Tiered-approval enforcement: a high-value entry must be posted via the
	// approvals workflow (which calls the service directly, bypassing this guard).
	if a.Cfg.RequireApproval {
		if draft, derr := a.Ledger.GetJournalEntry(c.Request.Context(), id); derr == nil && draft != nil {
			total := decimal.Zero
			for _, l := range draft.Lines {
				total = total.Add(l.Debit)
			}
			if a.ApprovalGuard(c, total) {
				return
			}
		}
	}
	entry, err := a.Ledger.PostJournalEntry(c.Request.Context(), id, chainActor(c))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ledger.ErrInvalidStatus) || errors.Is(err, ledger.ErrUnbalancedEntry) || errors.Is(err, ledger.ErrPeriodClosed) {
			status = http.StatusUnprocessableEntity
		}
		apierr.JSONStatus(c, status, err.Error())
		return
	}
	c.JSON(http.StatusOK, entry)
	logBusinessEvent(c, a.Audit, auditlog.EventJournalPosted, "journal_entry", entry.ID.String(), http.StatusOK, map[string]any{
		"entryNumber": entry.EntryNumber,
	})
}

type reverseEntryRequest struct {
	Reason string `json:"reason"`
}

// ReverseJournalEntry posts a reversing entry for a posted entry and marks the
// original 'reversed'. The only sanctioned way to undo a posted entry.
func (a *API) ReverseJournalEntry(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req reverseEntryRequest
	_ = c.ShouldBindJSON(&req)
	rev, err := a.Ledger.ReverseJournalEntry(c.Request.Context(), id, req.Reason, chainActor(c))
	if err != nil {
		switch {
		case errors.Is(err, ledger.ErrEntryNotFound):
			apierr.JSONStatus(c, http.StatusNotFound, "journal entry not found")
		case errors.Is(err, ledger.ErrNotReversible):
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "only a posted entry can be reversed")
		default:
			apierr.JSONStatus(c, http.StatusInternalServerError, "could not reverse entry")
		}
		return
	}
	c.JSON(http.StatusCreated, rev)
	logBusinessEvent(c, a.Audit, auditlog.EventJournalPosted, "journal_entry", rev.ID.String(), http.StatusCreated, map[string]any{
		"entryNumber": rev.EntryNumber, "reverses": id.String(),
	})
}

var periodRE = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// ListFiscalPeriods returns every period with an explicit open/closed status.
// Periods absent from the list are open by default.
func (a *API) ListFiscalPeriods(c *gin.Context) {
	items, err := a.Ledger.FiscalPeriods(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list periods")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "_note": "periods default to open; only closed/reopened periods appear here"})
}

// CloseFiscalPeriod blocks further postings dated in the given 'YYYY-MM'.
func (a *API) CloseFiscalPeriod(c *gin.Context) {
	a.setPeriodStatus(c, true)
}

// ReopenFiscalPeriod lifts a close so postings into the period are allowed again.
func (a *API) ReopenFiscalPeriod(c *gin.Context) {
	a.setPeriodStatus(c, false)
}

func (a *API) setPeriodStatus(c *gin.Context, close bool) {
	period := c.Param("period")
	if !periodRE.MatchString(period) {
		apierr.JSONStatus(c, http.StatusBadRequest, "period must be in YYYY-MM format")
		return
	}
	var by *uuid.UUID
	if raw, ok := c.Get("userID"); ok {
		if id, ok := raw.(uuid.UUID); ok {
			by = &id
		}
	}
	var err error
	event := auditlog.EventFiscalPeriodReopened
	if close {
		err = a.Ledger.ClosePeriod(c.Request.Context(), period, by)
		event = auditlog.EventFiscalPeriodClosed
	} else {
		err = a.Ledger.ReopenPeriod(c.Request.Context(), period, by)
	}
	if err != nil {
		if errors.Is(err, repository.ErrPeriodHasDrafts) {
			apierr.JSONStatus(c, http.StatusUnprocessableEntity, "period has draft entries; post or delete them before closing")
			return
		}
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not update period")
		return
	}
	status := "open"
	if close {
		status = "closed"
	}
	c.JSON(http.StatusOK, gin.H{"period": period, "status": status})
	logBusinessEvent(c, a.Audit, event, "fiscal_period", period, http.StatusOK, map[string]any{
		"period": period, "status": status,
	})
}

func (a *API) ListARItems(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListARItems(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list AR items")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListAPItems(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListAPItems(c.Request.Context(), limit, offset)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list AP items")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func pagination(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	return limit, offset
}

func parseLines(req []journalLineRequest) ([]ledger.LineInput, error) {
	out := make([]ledger.LineInput, 0, len(req))
	for _, l := range req {
		debit := decimal.Zero
		credit := decimal.Zero
		if l.Debit != "" {
			d, err := decimal.NewFromString(l.Debit)
			if err != nil {
				return nil, err
			}
			debit = d
		}
		if l.Credit != "" {
			cr, err := decimal.NewFromString(l.Credit)
			if err != nil {
				return nil, err
			}
			credit = cr
		}
		li := ledger.LineInput{
			AccountCode: l.AccountCode,
			Debit:       debit,
			Credit:      credit,
			Memo:        l.Memo,
		}
		if l.CostCenterID != "" {
			id, err := uuid.Parse(l.CostCenterID)
			if err != nil {
				return nil, err
			}
			li.CostCenterID = &id
		}
		if l.ProjectID != "" {
			id, err := uuid.Parse(l.ProjectID)
			if err != nil {
				return nil, err
			}
			li.ProjectID = &id
		}
		out = append(out, li)
	}
	return out, nil
}

type createOpenItemRequest struct {
	OrgID             string `json:"orgId"`
	BillingIdentityID string `json:"billingIdentityId"`
	CustomerRef       string `json:"customerRef"`
	VendorRef         string `json:"vendorRef"`
	DocumentRef       string `json:"documentRef" binding:"required"`
	Description       string `json:"description"`
	Amount            string `json:"amount" binding:"required"`
	Currency          string `json:"currency"`
	DueDate           string `json:"dueDate"`
}

func (a *API) CreateARItem(c *gin.Context) {
	var req createOpenItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	billing, err := a.resolveBillingCustomerRef(c.Request.Context(), billingResolveInput{
		OrgID:             req.OrgID,
		BillingIdentityID: req.BillingIdentityID,
		CustomerRef:       req.CustomerRef,
	})
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "UGX"
	}
	var due *time.Time
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid dueDate")
			return
		}
		due = &t
	}
	// The sale.completed event is written to the outbox in the same transaction
	// as the AR item, so the two can never diverge if the broker is down.
	outbox := saleCompletedOutbox(a.Events, req.DocumentRef, billing.CustomerRef, req.Amount, currency)
	var item *domain.AROpenItem
	if billing.BillingOrgID != nil {
		item, err = a.Ledger.CreateARItemWithBilling(c.Request.Context(), billing.CustomerRef, req.DocumentRef, req.Description, req.Amount, currency, due, billing.BillingOrgID, billing.BillingIdentityID, outbox)
	} else {
		item, err = a.Ledger.CreateARItem(c.Request.Context(), billing.CustomerRef, req.DocumentRef, req.Description, req.Amount, currency, due, outbox)
	}
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create AR item")
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) CreateAPItem(c *gin.Context) {
	var req createOpenItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "UGX"
	}
	var due *time.Time
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			apierr.JSONStatus(c, http.StatusBadRequest, "invalid dueDate")
			return
		}
		due = &t
	}
	outbox := invoicePostedOutbox(a.Events, req.DocumentRef, req.VendorRef, req.Amount, currency)
	item, err := a.Ledger.CreateAPItem(c.Request.Context(), req.VendorRef, req.DocumentRef, req.Description, req.Amount, currency, due, outbox)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not create AP item")
		return
	}
	c.JSON(http.StatusCreated, item)
}

// saleCompletedOutbox builds the sale.completed outbox event, or nil when event
// publishing is disabled or the inputs are incomplete (then nothing is enqueued).
func saleCompletedOutbox(bus *events.Bus, documentRef, customerRef, amount, currency string) *repository.OutboxEvent {
	if bus == nil || !bus.Enabled() || documentRef == "" || amount == "" {
		return nil
	}
	return &repository.OutboxEvent{
		Topic:        bus.FinanceTopic(),
		PartitionKey: documentRef,
		EventID:      events.TypeSaleCompleted + ":" + documentRef,
		EventType:    events.TypeSaleCompleted,
		Payload: map[string]any{
			"amount":      amount,
			"currency":    currency,
			"customerRef": customerRef,
			"documentRef": documentRef,
		},
	}
}

// invoicePostedOutbox builds the invoice.posted outbox event, or nil when
// disabled/incomplete.
func invoicePostedOutbox(bus *events.Bus, documentRef, vendorRef, amount, currency string) *repository.OutboxEvent {
	if bus == nil || !bus.Enabled() || documentRef == "" || amount == "" {
		return nil
	}
	return &repository.OutboxEvent{
		Topic:        bus.FinanceTopic(),
		PartitionKey: documentRef,
		EventID:      events.TypeInvoicePosted + ":" + documentRef,
		EventType:    events.TypeInvoicePosted,
		Payload: map[string]any{
			"amount":      amount,
			"currency":    currency,
			"vendorRef":   vendorRef,
			"documentRef": documentRef,
		},
	}
}

func (a *API) TrialBalance(c *gin.Context) {
	scope, err := a.entityScope(c)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not resolve entity scope")
		return
	}
	rows, err := a.Ledger.TrialBalance(c.Request.Context(), dateParam(c, "from"), dateParam(c, "to"), scope)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not build trial balance")
		return
	}
	// A trial balance exists to surface imbalance: total debits must equal total
	// credits. Compute and report it rather than leaving callers to re-sum.
	totalDebit, totalCredit := decimal.Zero, decimal.Zero
	for _, r := range rows {
		d, _ := decimal.NewFromString(r.Debit)
		cr, _ := decimal.NewFromString(r.Credit)
		totalDebit = totalDebit.Add(d)
		totalCredit = totalCredit.Add(cr)
	}
	c.JSON(http.StatusOK, gin.H{
		"rows":        rows,
		"status":      "posted_entries_only",
		"totalDebit":  totalDebit.StringFixed(2),
		"totalCredit": totalCredit.StringFixed(2),
		"balanced":    totalDebit.Equal(totalCredit),
	})
}
