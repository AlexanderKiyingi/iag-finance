package repository

import (
	"context"

	"github.com/google/uuid"
)

// LinkAROpenItemByDocumentRef attaches a posted journal entry to the matching AR
// open item. Only rows with a null journal_entry_id are updated (idempotent).
func (r *Repository) LinkAROpenItemByDocumentRef(ctx context.Context, documentRef string, journalEntryID uuid.UUID, sourceEventID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ar_open_items
		SET journal_entry_id = $2, source_event_id = $3, updated_at = NOW()
		WHERE document_ref = $1 AND journal_entry_id IS NULL
	`, documentRef, journalEntryID, sourceEventID)
	return err
}

// LinkAPOpenItemByDocumentRef attaches a posted journal entry to the matching AP
// open item. Only rows with a null journal_entry_id are updated (idempotent).
func (r *Repository) LinkAPOpenItemByDocumentRef(ctx context.Context, documentRef string, journalEntryID uuid.UUID, sourceEventID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ap_open_items
		SET journal_entry_id = $2, source_event_id = $3, updated_at = NOW()
		WHERE document_ref = $1 AND journal_entry_id IS NULL
	`, documentRef, journalEntryID, sourceEventID)
	return err
}
