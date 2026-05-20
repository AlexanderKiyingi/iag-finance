package tablerows

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Row struct {
	ID       int64     `json:"id"`
	TableID  string    `json:"tableId"`
	RowHTML  string    `json:"rowHTML"`
	Created  time.Time `json:"ts"`
}

type AppendBody struct {
	RowHTML string `json:"rowHTML" binding:"required"`
}

type Store struct {
	Pg *pgxpool.Pool
}

func (s *Store) List(ctx context.Context, tenant, tableID string) ([]Row, error) {
	rows, err := s.Pg.Query(ctx, `
		SELECT id, table_id, row_html, created_at
		FROM table_rows
		WHERE tenant_id = $1 AND table_id = $2
		ORDER BY id ASC
	`, tenant, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.TableID, &r.RowHTML, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Append(ctx context.Context, tenant, tableID, rowHTML string) (*Row, error) {
	var r Row
	err := s.Pg.QueryRow(ctx, `
		INSERT INTO table_rows (tenant_id, table_id, row_html)
		VALUES ($1, $2, $3)
		RETURNING id, table_id, row_html, created_at
	`, tenant, tableID, rowHTML).Scan(&r.ID, &r.TableID, &r.RowHTML, &r.Created)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
