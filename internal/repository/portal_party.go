package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrPortalPartyNotLinked = errors.New("portal party not linked")

func (r *Repository) UpsertPortalPartyLink(ctx context.Context, platformUserID, partyID uuid.UUID, partyBusinessID, supplierType string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO portal_party_links (platform_user_id, party_id, party_business_id, supplier_type, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (platform_user_id) DO UPDATE SET
			party_id = EXCLUDED.party_id,
			party_business_id = EXCLUDED.party_business_id,
			supplier_type = EXCLUDED.supplier_type,
			updated_at = now()`,
		platformUserID, partyID, strings.TrimSpace(partyBusinessID), strings.TrimSpace(supplierType))
	return err
}

func (r *Repository) PartyIDForPlatformUser(ctx context.Context, platformUserID uuid.UUID) (uuid.UUID, error) {
	var partyID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT party_id FROM portal_party_links WHERE platform_user_id = $1`, platformUserID).Scan(&partyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrPortalPartyNotLinked
	}
	return partyID, err
}
