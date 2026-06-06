package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type EFRISStatus struct {
	Pending      int `json:"pending"`
	Submitted    int `json:"submitted"`
	Acknowledged int `json:"acknowledged"`
	Failed       int `json:"failed"`
}

type BankingStatus struct {
	Imported    int `json:"imported"`
	Reconciling int `json:"reconciling"`
	Reconciled  int `json:"reconciled"`
	Failed      int `json:"failed"`
}

func (r *Repository) EFRISCounts(ctx context.Context) (EFRISStatus, error) {
	var s EFRISStatus
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending')::int,
			COUNT(*) FILTER (WHERE status = 'submitted')::int,
			COUNT(*) FILTER (WHERE status = 'acknowledged')::int,
			COUNT(*) FILTER (WHERE status = 'failed')::int
		FROM efris_submissions
	`).Scan(&s.Pending, &s.Submitted, &s.Acknowledged, &s.Failed)
	return s, err
}

func (r *Repository) BankingCounts(ctx context.Context) (BankingStatus, error) {
	var s BankingStatus
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'imported')::int,
			COUNT(*) FILTER (WHERE status = 'reconciling')::int,
			COUNT(*) FILTER (WHERE status = 'reconciled')::int,
			COUNT(*) FILTER (WHERE status = 'failed')::int
		FROM bank_statements
	`).Scan(&s.Imported, &s.Reconciling, &s.Reconciled, &s.Failed)
	return s, err
}

func (r *Repository) QueueEFRISSubmission(ctx context.Context, documentRef string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO efris_submissions (document_ref, status)
		VALUES ($1, 'pending')
		ON CONFLICT (document_ref) DO UPDATE SET updated_at = NOW()
		RETURNING id
	`, documentRef).Scan(&id)
	return id, err
}

func (r *Repository) ImportBankStatement(ctx context.Context, bankAccountCode string, statementDate time.Time, lineCount int) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO bank_statements (bank_account_code, statement_date, line_count, status)
		VALUES ($1, $2, $3, 'imported')
		RETURNING id
	`, bankAccountCode, statementDate, lineCount).Scan(&id)
	return id, err
}

func (r *Repository) UpdateEFRISSubmission(ctx context.Context, documentRef, status, receipt, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE efris_submissions
		SET status = $2, ura_receipt = NULLIF($3, ''), error_message = NULLIF($4, ''),
		    submitted_at = CASE WHEN $2 IN ('submitted', 'acknowledged') THEN NOW() ELSE submitted_at END,
		    updated_at = NOW()
		WHERE document_ref = $1
	`, documentRef, status, receipt, errMsg)
	return err
}

type StatementLineInput struct {
	Date        time.Time
	Description string
	Payee       string
	Amount      string
	Direction   string
	ExternalRef string
}

func (r *Repository) InsertStatementLines(ctx context.Context, statementID uuid.UUID, lines []StatementLineInput) (int, error) {
	count := 0
	for _, l := range lines {
		externalRef := strings.TrimSpace(l.ExternalRef)
		var tag pgconn.CommandTag
		var err error
		if externalRef != "" {
			tag, err = r.pool.Exec(ctx, `
				INSERT INTO bank_statement_lines (statement_id, line_date, description, payee, amount, direction, external_ref)
				VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)
				ON CONFLICT (external_ref) DO NOTHING
			`, statementID, l.Date, l.Description, l.Payee, l.Amount, l.Direction, externalRef)
		} else {
			tag, err = r.pool.Exec(ctx, `
				INSERT INTO bank_statement_lines (statement_id, line_date, description, payee, amount, direction, external_ref)
				VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)
			`, statementID, l.Date, l.Description, l.Payee, l.Amount, l.Direction, nil)
		}
		if err != nil {
			return count, err
		}
		if tag.RowsAffected() > 0 {
			count++
		}
	}
	_, err := r.pool.Exec(ctx, `UPDATE bank_statements SET line_count = $2, updated_at = NOW() WHERE id = $1`, statementID, count)
	return count, err
}

func (r *Repository) MaterializeBankTransactions(ctx context.Context, bankAccountCode string, statementID uuid.UUID) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, line_date, description, payee, amount::text, direction, matched_document_ref
		FROM bank_statement_lines WHERE statement_id = $1
	`, statementID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var lineID uuid.UUID
		var dt time.Time
		var desc, payee, amount, direction string
		var matched *string
		if err := rows.Scan(&lineID, &dt, &desc, &payee, &amount, &direction, &matched); err != nil {
			return err
		}
		action := "add"
		if matched != nil && *matched != "" {
			action = "match"
		}
		var spent, received *string
		if direction == "debit" {
			spent = &amount
		} else {
			received = &amount
		}
		_, err = r.pool.Exec(ctx, `
			INSERT INTO bank_transactions (bank_account_code, txn_date, description, payee, category, spent, received, action_label, matched_ref, statement_line_id)
			VALUES ($1, $2, $3, $4, '', $5::numeric, $6::numeric, $7, $8, $9)
			ON CONFLICT (statement_line_id) WHERE statement_line_id IS NOT NULL DO NOTHING
		`, bankAccountCode, dt, desc, payee, spent, received, action, matched, lineID)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}
