package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/integrations"
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
		_ = a.Ledger.CompleteEFRISSubmission(c.Request.Context(), req.DocumentRef, status, receipt, errMsg)
		if a.Events != nil && status == "acknowledged" {
			a.Events.Publish(c.Request.Context(), events.TypeEFRISSubmitted+":"+req.DocumentRef, events.TypeEFRISSubmitted, map[string]any{
				"documentRef": req.DocumentRef, "uraReceipt": receipt,
			}, req.DocumentRef)
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id.String(), "status": status, "uraReceipt": receipt})
}

type importBankStatementRequest struct {
	BankAccountCode string `json:"bankAccountCode" binding:"required"`
	StatementDate   string `json:"statementDate" binding:"required"`
	LineCount       int    `json:"lineCount"`
}

func (a *API) ImportBankStatement(c *gin.Context) {
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
	if req.LineCount < 0 {
		req.LineCount = 0
	}
	id, err := a.Ledger.ImportBankStatement(c.Request.Context(), req.BankAccountCode, stmtDate, req.LineCount)
	if err != nil {
		apierr.JSONStatus(c, http.StatusConflict, "could not import bank statement")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id.String(), "status": "imported"})
}
