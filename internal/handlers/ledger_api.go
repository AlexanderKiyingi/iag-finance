package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/integrations"
	"github.com/iag-finance/backend/internal/ledger"
)

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type API struct {
	Ledger          *ledger.Service
	Audit           *auditlog.Service
	DB              HealthChecker
	ConsumerEnabled bool
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

func (a *API) URAStatus(c *gin.Context) {
	c.JSON(http.StatusOK, integrations.URAStatus())
}

func (a *API) BankingStatus(c *gin.Context) {
	c.JSON(http.StatusOK, integrations.BankingStatus())
}

func (a *API) ListChartOfAccounts(c *gin.Context) {
	items, err := a.Ledger.ListChartOfAccounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list accounts"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parentId"})
			return
		}
		parentID = &id
	}
	acct, err := a.Ledger.CreateChartAccount(c.Request.Context(), req.Code, req.Name, req.AccountType, currency, parentID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "could not create account"})
		return
	}
	c.JSON(http.StatusCreated, acct)
	logBusinessEvent(c, a.Audit, auditlog.EventChartAccountCreated, "chart_of_account", acct.ID.String(), http.StatusCreated, map[string]any{
		"code": acct.Code, "name": acct.Name, "accountType": acct.AccountType,
	})
}

func (a *API) ListJournalEntries(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListJournalEntries(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list entries"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	entry, err := a.Ledger.GetJournalEntry(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load entry"})
		return
	}
	if entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, entry)
}

type journalLineRequest struct {
	AccountCode string `json:"accountCode" binding:"required"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Memo        string `json:"memo"`
}

type createJournalRequest struct {
	Description string               `json:"description" binding:"required"`
	Lines       []journalLineRequest `json:"lines" binding:"required,min=2"`
}

func (a *API) CreateJournalEntry(c *gin.Context) {
	var req createJournalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lines, err := parseLines(req.Lines)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(status, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	entry, err := a.Ledger.PostJournalEntry(c.Request.Context(), id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ledger.ErrInvalidStatus) || errors.Is(err, ledger.ErrUnbalancedEntry) {
			status = http.StatusUnprocessableEntity
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entry)
	logBusinessEvent(c, a.Audit, auditlog.EventJournalPosted, "journal_entry", entry.ID.String(), http.StatusOK, map[string]any{
		"entryNumber": entry.EntryNumber,
	})
}

func (a *API) ListARItems(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListARItems(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list AR items"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) ListAPItems(c *gin.Context) {
	limit, offset := pagination(c)
	items, err := a.Ledger.ListAPItems(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list AP items"})
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
		out = append(out, ledger.LineInput{
			AccountCode: l.AccountCode,
			Debit:       debit,
			Credit:      credit,
			Memo:        l.Memo,
		})
	}
	return out, nil
}

type createOpenItemRequest struct {
	CustomerRef string `json:"customerRef"`
	VendorRef   string `json:"vendorRef"`
	DocumentRef string `json:"documentRef" binding:"required"`
	Description string `json:"description"`
	Amount      string `json:"amount" binding:"required"`
	Currency    string `json:"currency"`
	DueDate     string `json:"dueDate"`
}

func (a *API) CreateARItem(c *gin.Context) {
	var req createOpenItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dueDate"})
			return
		}
		due = &t
	}
	item, err := a.Ledger.CreateARItem(c.Request.Context(), req.CustomerRef, req.DocumentRef, req.Description, req.Amount, currency, due)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "could not create AR item"})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) CreateAPItem(c *gin.Context) {
	var req createOpenItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dueDate"})
			return
		}
		due = &t
	}
	item, err := a.Ledger.CreateAPItem(c.Request.Context(), req.VendorRef, req.DocumentRef, req.Description, req.Amount, currency, due)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "could not create AP item"})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (a *API) TrialBalance(c *gin.Context) {
	rows, err := a.Ledger.TrialBalance(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build trial balance"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "status": "posted_entries_only"})
}
