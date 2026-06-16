package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/integrations"
	"github.com/iag-finance/backend/internal/repository"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

func (a *API) URAStatus(c *gin.Context) {
	counts, err := a.Ledger.EFRISCounts(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not load EFRIS status")
		return
	}
	mode := "stub"
	if a.Integrations != nil {
		mode = a.Integrations.EFRISMode()
	}
	c.JSON(http.StatusOK, gin.H{
		"name":        "ura-efris",
		"adapter":     mode,
		"description": efrisDescription(mode),
		"counts":      counts,
		"checkedAt":   time.Now().UTC(),
	})
}

func (a *API) BankingStatus(c *gin.Context) {
	counts, err := a.Ledger.BankingCounts(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not load banking status")
		return
	}
	mode := "stub"
	if a.Integrations != nil {
		mode = a.Integrations.BankMode()
	}
	c.JSON(http.StatusOK, gin.H{
		"name":        "banking",
		"adapter":     mode,
		"description": bankDescription(mode),
		"counts":      counts,
		"checkedAt":   time.Now().UTC(),
	})
}

func efrisDescription(mode string) string {
	switch mode {
	case "http":
		return "URA EFRIS HTTP adapter active"
	case "ura_s2s":
		return "URA EFRIS server-to-server (T109) adapter active"
	case "simulate":
		return "URA EFRIS simulate mode (dev)"
	default:
		return "URA EFRIS not configured — set URA_EFRIS_MODE, URA_EFRIS_BASE_URL, or URA_EFRIS_SIMULATE=true"
	}
}

func bankDescription(mode string) string {
	switch mode {
	case "http":
		return "Bank feed HTTP adapter active"
	case "simulate":
		return "Bank feed simulate mode (dev)"
	default:
		return "Bank feed not configured — set BANK_FEED_BASE_URL or BANK_FEED_SIMULATE=true"
	}
}

type submitEFRISRequest struct {
	DocumentRef string `json:"documentRef" binding:"required"`
}

func (a *API) SubmitEFRIS(c *gin.Context) {
	var req submitEFRISRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	// Idempotency: an already-acknowledged document is a fiscalised invoice —
	// never re-submit it to URA (that would file a duplicate fiscal receipt).
	if existing, err := a.Ledger.GetEFRISSubmission(c.Request.Context(), req.DocumentRef); err == nil &&
		existing.Found && existing.Status == "acknowledged" {
		c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "uraReceipt": existing.Receipt, "idempotent": true})
		return
	}
	id, err := a.Ledger.QueueEFRISSubmission(c.Request.Context(), req.DocumentRef)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not queue EFRIS submission")
		return
	}
	status := "pending"
	receipt := ""
	errMsg := ""
	if a.Integrations != nil && a.Integrations.EFRIS != nil {
		ar, _ := a.Ledger.GetARByDocumentRef(c.Request.Context(), req.DocumentRef)
		submitReq := integrations.EFRISSubmitRequest{
			DocumentRef: req.DocumentRef,
			Currency:    "UGX",
		}
		if ar != nil {
			submitReq.Amount = ar.Amount
			submitReq.Currency = ar.Currency
			submitReq.CustomerRef = ar.CustomerRef
		}
		res, submitErr := a.Integrations.EFRIS.Submit(c.Request.Context(), submitReq)
		if submitErr != nil {
			status = "failed"
			errMsg = submitErr.Error()
		} else {
			status = res.Status
			if status == "" {
				status = "submitted"
			}
			receipt = res.URAReceipt
			if res.ErrorMessage != "" {
				errMsg = res.ErrorMessage
				status = "failed"
			}
		}
		if err := a.Ledger.CompleteEFRISSubmission(c.Request.Context(), req.DocumentRef, status, receipt, errMsg); err != nil {
			// URA may have accepted the submission while our state write failed —
			// surface it so the divergence is visible and reconcilable.
			slog.Error("efris submission state write failed", "documentRef", req.DocumentRef, "status", status, "err", err)
		}
		if a.Events != nil && a.Events.Enabled() && status == "acknowledged" {
			// Durable via the outbox so a broker outage doesn't drop the event.
			if err := a.Ledger.EnqueueOutbox(c.Request.Context(), repository.OutboxEvent{
				Topic:        a.Events.FinanceTopic(),
				PartitionKey: req.DocumentRef,
				EventID:      events.TypeEFRISSubmitted + ":" + req.DocumentRef,
				EventType:    events.TypeEFRISSubmitted,
				Payload:      map[string]any{"documentRef": req.DocumentRef, "uraReceipt": receipt},
			}); err != nil {
				slog.Error("efris event enqueue failed", "documentRef", req.DocumentRef, "err", err)
			}
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id.String(), "status": status, "uraReceipt": receipt})
}

type importBankStatementRequest struct {
	BankAccountCode string `json:"bankAccountCode" binding:"required"`
	StatementDate   string `json:"statementDate" binding:"required"`
}

// ImportBankStatement pulls the real statement lines for the given day from the
// configured bank adapter and persists them (deduped). It no longer trusts a
// client-supplied line count — the count is whatever was actually imported.
func (a *API) ImportBankStatement(c *gin.Context) {
	if a.Integrations == nil || a.Integrations.Bank == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "bank feed not configured")
		return
	}
	var req importBankStatementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	stmtDate, err := time.Parse("2006-01-02", req.StatementDate)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "statementDate must be YYYY-MM-DD")
		return
	}
	from := stmtDate.UTC()
	to := from.AddDate(0, 0, 1)

	lines, err := a.Integrations.Bank.FetchLines(c.Request.Context(), req.BankAccountCode, from, to)
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadGateway, err.Error())
		return
	}
	inputs := make([]repository.StatementLineInput, 0, len(lines))
	for _, l := range lines {
		inputs = append(inputs, repository.StatementLineInput{
			Date: l.Date, Description: l.Description, Payee: l.Payee,
			Amount: l.Amount, Direction: l.Direction, ExternalRef: l.ExternalRef,
		})
	}
	stmtID, n, err := a.Ledger.SyncBankFeed(c.Request.Context(), req.BankAccountCode, from, to, inputs)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not import bank statement")
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id": stmtID, "status": "imported", "lines": n, "adapter": a.Integrations.Bank.Mode(),
	})
}
