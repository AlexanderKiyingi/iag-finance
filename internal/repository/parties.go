package repository

import (
	"context"

	"github.com/google/uuid"
)

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
}

func (r *Repository) createParty(ctx context.Context, table, code, name, email, phone, currency string) (*Party, error) {
	if currency == "" {
		currency = "UGX"
	}
	var p Party
	err := r.pool.QueryRow(ctx,
		"INSERT INTO "+table+" (entity_id, code, name, email, phone, currency) VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6) "+
			"RETURNING id, code, name, COALESCE(email,''), COALESCE(phone,''), currency, active",
		EntityFromContext(ctx), code, name, email, phone, currency).
		Scan(&p.ID, &p.Code, &p.Name, &p.Email, &p.Phone, &p.Currency, &p.Active)
	if err != nil {
		return nil, err
	}
	return &p, nil
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
	return r.createParty(ctx, "customers", code, name, email, phone, currency)
}
func (r *Repository) ListCustomers(ctx context.Context) ([]Party, error) {
	return r.listParties(ctx, "customers")
}
func (r *Repository) CreateVendor(ctx context.Context, code, name, email, phone, currency string) (*Party, error) {
	return r.createParty(ctx, "vendors", code, name, email, phone, currency)
}
func (r *Repository) ListVendors(ctx context.Context) ([]Party, error) {
	return r.listParties(ctx, "vendors")
}
