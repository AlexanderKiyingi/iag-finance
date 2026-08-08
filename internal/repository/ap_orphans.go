package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Orphaned AP journals: entries that credit the payables control account but
// have no open item against them.
//
// The payable exists in the general ledger and is invisible to the subledger —
// absent from aged payables, with no vendor to pay, and leaving a permanent
// unexplained difference on the control account. Every fleet fuel record booked
// before the payable was created is in this state.
//
// The query looks for the condition itself rather than for a particular source,
// so it finds any future handler that books a payable without recording one.

// APOrphanRow is one journal crediting AP with nothing in the subledger.
type APOrphanRow struct {
	JournalEntryID uuid.UUID  `json:"journalEntryId"`
	EntryNumber    string     `json:"entryNumber"`
	Description    string     `json:"description"`
	SourceService  string     `json:"sourceService,omitempty"`
	SourceEventID  string     `json:"sourceEventId,omitempty"`
	APCredit       string     `json:"apCredit"`
	PostedAt       *time.Time `json:"postedAt,omitempty"`
}

// APOrphanSummary is the size of the problem: what the control account carries
// that the subledger cannot explain.
type APOrphanSummary struct {
	Count       int    `json:"count"`
	TotalCredit string `json:"totalCredit"`
}

const apControlCode = "2000"

// ListOrphanedAPJournals returns journals crediting AP with no open item.
func (r *Repository) ListOrphanedAPJournals(ctx context.Context, limit int) ([]APOrphanRow, *APOrphanSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	summary := &APOrphanSummary{}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(t.ap_credit), 0)::text
		FROM (
			SELECT je.id, SUM(jl.credit) AS ap_credit
			FROM journal_entries je
			JOIN journal_lines jl ON jl.journal_entry_id = je.id
			JOIN chart_of_accounts coa ON coa.id = jl.account_id
			WHERE coa.code = $1 AND jl.credit > 0
			  AND NOT EXISTS (
			      SELECT 1 FROM ap_open_items ap WHERE ap.journal_entry_id = je.id
			  )
			GROUP BY je.id
		) t`, apControlCode).Scan(&summary.Count, &summary.TotalCredit); err != nil {
		return nil, nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT je.id, je.entry_number, je.description,
		       COALESCE(je.source_service, ''), COALESCE(je.source_event_id, ''),
		       SUM(jl.credit)::text, je.posted_at
		FROM journal_entries je
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		WHERE coa.code = $1 AND jl.credit > 0
		  AND NOT EXISTS (
		      SELECT 1 FROM ap_open_items ap WHERE ap.journal_entry_id = je.id
		  )
		GROUP BY je.id, je.entry_number, je.description, je.source_service,
		         je.source_event_id, je.posted_at
		ORDER BY je.posted_at DESC NULLS LAST
		LIMIT $2`, apControlCode, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := []APOrphanRow{}
	for rows.Next() {
		var o APOrphanRow
		if err := rows.Scan(&o.JournalEntryID, &o.EntryNumber, &o.Description,
			&o.SourceService, &o.SourceEventID, &o.APCredit, &o.PostedAt); err != nil {
			return nil, nil, err
		}
		out = append(out, o)
	}
	return out, summary, rows.Err()
}

// Contract payables and the currency they were booked in.
//
// Finance hardcoded UGX when creating a payable from contracts.payment.authorized,
// and contract-management did not send the contract's currency, so every payment
// on a non-UGX contract was booked as the same number of shillings. Both sides
// are fixed, but entries made before that are still wrong in the ledger and
// finance cannot tell which: the true currency lives on the contract.
//
// This lists them so they can be reconciled against contract-management. The
// documentRef prefix is finance's own convention for contract payables, set
// where the item is created, not a guess at someone else's format.

// ContractPayableRow is one payable raised from a contract payment.
type ContractPayableRow struct {
	DocumentRef string    `json:"documentRef"`
	VendorRef   string    `json:"vendorRef"`
	Description string    `json:"description"`
	Amount      string    `json:"amount"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ContractPayableCurrency is the count and total booked under one currency.
type ContractPayableCurrency struct {
	Currency string `json:"currency"`
	Count    int    `json:"count"`
	Total    string `json:"total"`
}

const contractPayablePrefix = "CT-PAY-%"

// ListContractPayables returns contract-sourced payables, newest first, with a
// per-currency summary.
//
// The summary is the reconciliation: a deployment whose contracts are all in
// shillings should see one currency and nothing to check. Anything else is the
// list of entries to compare against the contracts that raised them.
func (r *Repository) ListContractPayables(ctx context.Context, currency string, limit int) (
	[]ContractPayableRow, []ContractPayableCurrency, error) {

	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	summaryRows, err := r.pool.Query(ctx, `
		SELECT currency, COUNT(*), COALESCE(SUM(amount), 0)::text
		FROM ap_open_items
		WHERE document_ref LIKE $1
		GROUP BY currency
		ORDER BY COUNT(*) DESC`, contractPayablePrefix)
	if err != nil {
		return nil, nil, err
	}
	summary := []ContractPayableCurrency{}
	for summaryRows.Next() {
		var c ContractPayableCurrency
		if err := summaryRows.Scan(&c.Currency, &c.Count, &c.Total); err != nil {
			summaryRows.Close()
			return nil, nil, err
		}
		summary = append(summary, c)
	}
	summaryRows.Close()
	if err := summaryRows.Err(); err != nil {
		return nil, nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT document_ref, COALESCE(vendor_ref, ''), COALESCE(description, ''),
		       amount::text, currency, status, created_at
		FROM ap_open_items
		WHERE document_ref LIKE $1
		  AND ($2 = '' OR currency = $2)
		ORDER BY created_at DESC
		LIMIT $3`, contractPayablePrefix, currency, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := []ContractPayableRow{}
	for rows.Next() {
		var p ContractPayableRow
		if err := rows.Scan(&p.DocumentRef, &p.VendorRef, &p.Description,
			&p.Amount, &p.Currency, &p.Status, &p.CreatedAt); err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	return out, summary, rows.Err()
}
