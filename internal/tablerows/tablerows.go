package tablerows

import (
	"context"
	"html"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Row struct {
	ID      int64     `json:"id"`
	TableID string    `json:"tableId"`
	RowHTML string    `json:"rowHTML"`
	Created time.Time `json:"ts"`
}

type AppendBody struct {
	RowHTML string `json:"rowHTML" binding:"required"`
}

type Store struct {
	Pg *pgxpool.Pool
}

func (s *Store) List(ctx context.Context, tableID string) ([]Row, error) {
	rows, err := s.Pg.Query(ctx, `
		SELECT id, table_id, row_html, created_at
		FROM table_rows
		WHERE table_id = $1
		ORDER BY id ASC
	`, tableID)
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

func (s *Store) Append(ctx context.Context, tableID, rowHTML string) (*Row, error) {
	// Escape on store so caller-supplied markup cannot execute when the
	// (deprecated) prototype UI replays it — neutralises stored XSS.
	rowHTML = html.EscapeString(rowHTML)
	var r Row
	err := s.Pg.QueryRow(ctx, `
		INSERT INTO table_rows (table_id, row_html)
		VALUES ($1, $2)
		RETURNING id, table_id, row_html, created_at
	`, tableID, rowHTML).Scan(&r.ID, &r.TableID, &r.RowHTML, &r.Created)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
