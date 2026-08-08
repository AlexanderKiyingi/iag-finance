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
	JournalEntryID uuid.UUID `json:"journalEntryId"`
	EntryNumber    string    `json:"entryNumber"`
	Description    string    `json:"description"`
	SourceService  string    `json:"sourceService,omitempty"`
	SourceEventID  string    `json:"sourceEventId,omitempty"`
	APCredit       string    `json:"apCredit"`
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
