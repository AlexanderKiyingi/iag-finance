package repository

import "context"

// Bank is a reference entry for the frontend "Bank Name" dropdown: a licensed
// bank, a mobile-money wallet, or petty cash. It is a flat lookup list, not a
// bank *account* (those are chart-of-accounts / banking records).
type Bank struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Active   bool   `json:"active"`
}

// ListBanks returns the active bank reference list in display order. Global
// (not entity-scoped): the same payment institutions apply to every entity.
func (r *Repository) ListBanks(ctx context.Context) ([]Bank, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT code, name, category, active FROM banks WHERE active ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bank
	for rows.Next() {
		var b Bank
		if err := rows.Scan(&b.Code, &b.Name, &b.Category, &b.Active); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
