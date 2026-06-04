package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// SyncAPPartyFromSCM backfills party_id on AP open items when SCM emits scm.party.* events.
func (r *Repository) SyncAPPartyFromSCM(ctx context.Context, partyID uuid.UUID, businessID, name string) (int64, error) {
	businessID = strings.TrimSpace(businessID)
	name = strings.TrimSpace(name)
	if partyID == uuid.Nil {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE ap_open_items SET party_id = $1
		WHERE party_id IS NULL AND (
			vendor_ref = $2 OR vendor_ref = $3 OR vendor_ref ILIKE $3
		)`, partyID, businessID, name)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
