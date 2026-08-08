package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
)

// POS ingest.
//
// There is no POS microservice on the platform, so till takings had no route to
// the ledger at all — the one domain creating financial records with nowhere to
// record them. Rather than stand up a service to relay them, the POS backend
// posts its receipts here directly.
//
// This is the synchronous style of ADR 0001: the caller knows a till sale is
// revenue, it is not asking finance to interpret an ambiguous fact. So it sends
// a caller-stable reference per receipt and that reference is the idempotency
// key — a till that loses its connection mid-upload can send the whole batch
// again and the already-booked receipts are reported as duplicates rather than
// posted twice.

// Tender decides which asset account a receipt debits. Cash is in the drawer;
// card and mobile money are owed by the acquirer until they settle, so they land
// in the same clearing account the payments bridge later relieves.
const (
	posAccountCash      = "1000" // Cash
	posAccountClearing  = "1050" // Payments Clearing
	posAccountRevenue   = "4000" // Revenue
	posAccountOutputVAT = "2100" // Output VAT
)

type posReceipt struct {
	// Reference is the till's own receipt number and must be stable across
	// retries. It is what makes re-uploading a batch safe.
	Reference string `json:"reference" binding:"required"`
	// Type is "sale" or "return". A return reverses the sale postings.
	Type string `json:"type"`
	// Gross is the total the customer paid, VAT included.
	Gross string `json:"gross" binding:"required"`
	// VAT is the output VAT within Gross. Zero or absent books the whole
	// amount as revenue.
	VAT string `json:"vat"`
	// Tender is "cash", "card" or "mobile". Anything else is treated as cash,
	// because the money is in the drawer unless the till says otherwise.
	Tender     string `json:"tender"`
	Currency   string `json:"currency"`
	OccurredAt string `json:"occurredAt"`
}

type posBatch struct {
	TerminalID string       `json:"terminalId"`
	Receipts   []posReceipt `json:"receipts" binding:"required,min=1"`
}

type posResult struct {
	Reference string `json:"reference"`
	Status    string `json:"status"` // posted | duplicate | rejected
	EntryID   string `json:"entryId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// IngestPOSReceipts books a batch of till receipts.
//
// The batch is not atomic and deliberately so: one malformed receipt should not
// stop the rest of a day's takings from reaching the books. Each receipt is
// reported individually, and a caller that retries gets the same answer.
func (a *API) IngestPOSReceipts(c *gin.Context) {
	var batch posBatch
	if err := c.ShouldBindJSON(&batch); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}

	terminal := strings.TrimSpace(batch.TerminalID)
	results := make([]posResult, 0, len(batch.Receipts))
	posted, duplicates, rejected := 0, 0, 0

	for _, r := range batch.Receipts {
		res := a.ingestOnePOSReceipt(c, terminal, r)
		switch res.Status {
		case "posted":
			posted++
		case "duplicate":
			duplicates++
		default:
			rejected++
		}
		results = append(results, res)
	}

	// 200 even with rejects: the caller needs the per-receipt verdict to decide
	// what to resend, and a blanket error would hide the ones that did post.
	c.JSON(http.StatusOK, gin.H{
		"terminalId": terminal,
		"posted":     posted,
		"duplicates": duplicates,
		"rejected":   rejected,
		"results":    results,
	})
}

// posLines turns a receipt into balanced journal lines. Pure, so the posting
// shape is testable without a ledger or a database behind it.
func posLines(r posReceipt) ([]ledger.LineInput, error) {
	gross, err := decimal.NewFromString(strings.TrimSpace(r.Gross))
	if err != nil || gross.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("gross must be a positive amount")
	}
	vat := decimal.Zero
	if s := strings.TrimSpace(r.VAT); s != "" {
		v, verr := decimal.NewFromString(s)
		if verr != nil {
			return nil, errors.New("vat is not a number")
		}
		vat = v
	}
	if vat.IsNegative() || vat.GreaterThanOrEqual(gross) {
		// VAT at or above the gross would leave revenue zero or negative, which
		// means the till sent them the wrong way round. Refuse rather than book
		// a nonsense entry.
		return nil, errors.New("vat must be less than gross")
	}

	tenderAccount := posAccountCash
	switch strings.ToLower(strings.TrimSpace(r.Tender)) {
	case "card", "mobile", "momo", "mobile_money":
		tenderAccount = posAccountClearing
	}

	net := gross.Sub(vat)
	var lines []ledger.LineInput
	if strings.EqualFold(strings.TrimSpace(r.Type), "return") {
		// Money leaves the drawer and revenue is given back.
		lines = append(lines, ledger.LineInput{AccountCode: posAccountRevenue, Debit: net, Memo: "POS return"})
		if vat.IsPositive() {
			lines = append(lines, ledger.LineInput{AccountCode: posAccountOutputVAT, Debit: vat, Memo: "POS return output VAT"})
		}
		lines = append(lines, ledger.LineInput{AccountCode: tenderAccount, Credit: gross, Memo: "POS refund"})
		return lines, nil
	}
	lines = append(lines, ledger.LineInput{AccountCode: tenderAccount, Debit: gross, Memo: "POS takings"})
	lines = append(lines, ledger.LineInput{AccountCode: posAccountRevenue, Credit: net, Memo: "POS revenue"})
	if vat.IsPositive() {
		lines = append(lines, ledger.LineInput{AccountCode: posAccountOutputVAT, Credit: vat, Memo: "POS output VAT"})
	}
	return lines, nil
}

func (a *API) ingestOnePOSReceipt(c *gin.Context, terminal string, r posReceipt) posResult {
	ref := strings.TrimSpace(r.Reference)
	out := posResult{Reference: ref}
	if ref == "" {
		out.Status, out.Error = "rejected", "reference is required"
		return out
	}
	lines, err := posLines(r)
	if err != nil {
		out.Status, out.Error = "rejected", err.Error()
		return out
	}

	desc := "POS " + strings.ToLower(strings.TrimSpace(r.Type))
	if desc == "POS " {
		desc = "POS sale"
	}
	desc += " — " + ref
	if terminal != "" {
		desc += " (" + terminal + ")"
	}

	currency := strings.TrimSpace(r.Currency)
	// The receipt reference is the idempotency key: BookFromEvent dedupes on it,
	// so a resent batch collides with the original booking instead of doubling
	// the day's takings.
	entry, err := a.Ledger.BookFromEvent(
		c.Request.Context(), ref, "pos.receipt", "iag.pos", terminal, desc, currency, lines)
	if err != nil {
		if errors.Is(err, ledger.ErrDuplicateEvent) {
			out.Status = "duplicate"
			return out
		}
		out.Status, out.Error = "rejected", err.Error()
		return out
	}
	out.Status = "posted"
	if entry != nil {
		out.EntryID = entry.ID.String()
	}
	return out
}
