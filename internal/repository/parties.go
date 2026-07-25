package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// vendorUpsertedType is the canonical cross-service vendor master event emitted
// on the finance topic (kept local to avoid an events→repository import cycle).
const vendorUpsertedType = "party.vendor.upserted"

// Party is a customer or vendor billing-party master record. It mirrors the
// lightweight Dimension shape but carries the contact/currency fields a billing
// party needs (and that the frontend create-new dialog collects).
type Party struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Email    string    `json:"email,omitempty"`
	Phone    string    `json:"phone,omitempty"`
	Currency string    `json:"currency"`
	Active   bool      `json:"active"`
	PartyID  uuid.UUID `json:"partyId,omitempty"`
}

// createParty inserts a billing party and returns it. It mints the shared
// party_id up front. When emitVendorEvent is true (vendors table + vendor sync
// enabled), it also enqueues party.vendor.upserted in the same tx so the master
// change and its cross-service notification commit atomically.
func (r *Repository) createParty(ctx context.Context, table, code, name, email, phone, currency string, emitVendorEvent bool) (*Party, error) {
	if currency == "" {
		currency = "UGX"
	}
	partyID := uuid.New()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var p Party
	err = tx.QueryRow(ctx,
		"INSERT INTO "+table+" (entity_id, code, name, email, phone, currency, party_id) VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, $7) "+
			"RETURNING id, code, name, COALESCE(email,''), COALESCE(phone,''), currency, active, party_id",
		EntityFromContext(ctx), code, name, email, phone, currency, partyID).
		Scan(&p.ID, &p.Code, &p.Name, &p.Email, &p.Phone, &p.Currency, &p.Active, &p.PartyID)
	if err != nil {
		return nil, err
	}

	if emitVendorEvent && r.vendorSyncTopic != "" {
		status := "Active"
		if !p.Active {
			status = "Inactive"
		}
		if err := enqueueOutboxTx(ctx, tx, OutboxEvent{
			Topic:        r.vendorSyncTopic,
			PartitionKey: p.PartyID.String(),
			EventID:      uuid.NewString(),
			EventType:    vendorUpsertedType,
			Payload: map[string]any{
				"party_id":      p.PartyID.String(),
				"code":          p.Code,
				"name":          p.Name,
				"email":         p.Email,
				"phone":         p.Phone,
				"currency":      p.Currency,
				"status":        status,
				"supplier_type": "vendor",
				"source":        "iag.finance",
			},
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertVendorByParty applies an inbound party.vendor.upserted (from procurement
// or SCM) to the finance vendor master, keyed on the shared party_id. It matches
// an existing row by party_id first, then by (entity_id, code), updating in
// place; otherwise it inserts a new vendor. It NEVER enqueues an outgoing event
// — the mesh's loop-prevention rule (emit only on local API mutations).
func (r *Repository) UpsertVendorByParty(ctx context.Context, entityID, partyID uuid.UUID, code, name, email, phone, currency, status string) error {
	if partyID == uuid.Nil {
		return nil
	}
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if currency == "" {
		currency = "UGX"
	}
	if code == "" {
		// Fall back to a party-derived code so the NOT NULL/unique (entity,code)
		// constraint holds when the source omitted a natural key.
		code = "PARTY-" + partyID.String()[:8]
	}
	active := !strings.EqualFold(strings.TrimSpace(status), "Inactive")

	// Update an existing row identified by party_id or by the natural key.
	tag, err := r.pool.Exec(ctx, `
		UPDATE vendors SET
			party_id = $2,
			name = CASE WHEN $4 <> '' THEN $4 ELSE name END,
			email = COALESCE(NULLIF($5,''), email),
			phone = COALESCE(NULLIF($6,''), phone),
			active = $7
		WHERE entity_id = $1 AND (party_id = $2 OR code = $3)`,
		entityID, partyID, code, name, email, phone, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	// No local row — insert. Ignore a concurrent insert on the same key.
	_, err = r.pool.Exec(ctx, `
		INSERT INTO vendors (entity_id, code, name, email, phone, currency, active, party_id)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, $7, $8)
		ON CONFLICT DO NOTHING`,
		entityID, code, name, email, phone, currency, active, partyID)
	return err
}

func (r *Repository) listParties(ctx context.Context, table string) ([]Party, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, code, name, COALESCE(email,''), COALESCE(phone,''), currency, active FROM "+table+
			" WHERE entity_id = $1 AND active ORDER BY name",
		EntityFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Party
	for rows.Next() {
		var p Party
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Email, &p.Phone, &p.Currency, &p.Active); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) CreateCustomer(ctx context.Context, code, name, email, phone, currency string) (*Party, error) {
	return r.createParty(ctx, "customers", code, name, email, phone, currency, false)
}
func (r *Repository) ListCustomers(ctx context.Context) ([]Party, error) {
	return r.listParties(ctx, "customers")
}
// CustomerEmailByRef resolves a customer's email from an AR document's
// customer_ref, which may be either the customer code or name. Returns "" when
// no matching customer has an email on file.
func (r *Repository) CustomerEmailByRef(ctx context.Context, ref string) (string, error) {
	var email string
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(email, '') FROM customers
		 WHERE entity_id = $1 AND (code = $2 OR name = $2) AND COALESCE(email, '') <> ''
		 ORDER BY (code = $2) DESC LIMIT 1`,
		EntityFromContext(ctx), ref).Scan(&email)
	if err != nil {
		return "", nil // no match (including no-rows) → no email, not an error
	}
	return email, nil
}

func (r *Repository) CreateVendor(ctx context.Context, code, name, email, phone, currency string) (*Party, error) {
	return r.createParty(ctx, "vendors", code, name, email, phone, currency, true)
}
func (r *Repository) ListVendors(ctx context.Context) ([]Party, error) {
	return r.listParties(ctx, "vendors")
}
