package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrStatementLineNotFound = errors.New("statement line not found")

type StatementLine struct {
	ID                  uuid.UUID `json:"id"`
	StatementID         uuid.UUID `json:"statementId"`
	LineDate            string    `json:"lineDate"`
	Description         string    `json:"description"`
	Payee               string    `json:"payee"`
	Amount              string    `json:"amount"`
	Direction           string    `json:"direction"`
	ExternalRef         string    `json:"externalRef,omitempty"`
	MatchStatus         string    `json:"matchStatus"`
	MatchedDocumentRef  string    `json:"matchedDocumentRef,omitempty"`
}

func (r *Repository) ListStatementLines(ctx context.Context, statementID uuid.UUID) ([]StatementLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, statement_id, line_date::text, description, payee, amount::text, direction,
		       COALESCE(external_ref, ''), match_status, COALESCE(matched_document_ref, '')
		FROM bank_statement_lines
		WHERE statement_id = $1
		ORDER BY line_date, id
	`, statementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatementLine
	for rows.Next() {
		var l StatementLine
		if err := rows.Scan(
			&l.ID, &l.StatementID, &l.LineDate, &l.Description, &l.Payee, &l.Amount, &l.Direction,
			&l.ExternalRef, &l.MatchStatus, &l.MatchedDocumentRef,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repository) GetStatementLine(ctx context.Context, lineID uuid.UUID) (*StatementLine, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, statement_id, line_date::text, description, payee, amount::text, direction,
		       COALESCE(external_ref, ''), match_status, COALESCE(matched_document_ref, '')
		FROM bank_statement_lines WHERE id = $1
	`, lineID)
	var l StatementLine
	if err := row.Scan(
		&l.ID, &l.StatementID, &l.LineDate, &l.Description, &l.Payee, &l.Amount, &l.Direction,
		&l.ExternalRef, &l.MatchStatus, &l.MatchedDocumentRef,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &l, nil
}

func (r *Repository) MatchStatementLine(ctx context.Context, lineID uuid.UUID, documentRef string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE bank_statement_lines
		SET match_status = 'matched', matched_document_ref = $2
		WHERE id = $1 AND match_status = 'unmatched'
	`, lineID, documentRef)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStatementLineNotFound
	}
	return nil
}

func (r *Repository) BankAccountCodeForStatement(ctx context.Context, statementID uuid.UUID) (string, error) {
	var code string
	err := r.pool.QueryRow(ctx, `
		SELECT bank_account_code FROM bank_statements WHERE id = $1
	`, statementID).Scan(&code)
	return code, err
}

func (r *Repository) FindDocumentRefByPaymentRef(ctx context.Context, paymentRef string) (string, string, error) {
	paymentRef = strings.TrimSpace(paymentRef)
	if paymentRef == "" {
		return "", "", nil
	}
	var docRef, direction string
	err := r.pool.QueryRow(ctx, `
		SELECT o.document_ref, p.direction
		FROM finance_payments p
		JOIN ar_open_items o ON p.direction = 'ar' AND p.open_item_id = o.id
		WHERE p.payment_ref = $1
		LIMIT 1
	`, paymentRef).Scan(&docRef, &direction)
	if err == nil {
		return docRef, direction, nil
	}
	if err != pgx.ErrNoRows {
		return "", "", err
	}
	err = r.pool.QueryRow(ctx, `
		SELECT o.document_ref, p.direction
		FROM finance_payments p
		JOIN ap_open_items o ON p.direction = 'ap' AND p.open_item_id = o.id
		WHERE p.payment_ref = $1
		LIMIT 1
	`, paymentRef).Scan(&docRef, &direction)
	if err == pgx.ErrNoRows {
		return "", "", nil
	}
	return docRef, direction, err
}

func (r *Repository) FindOpenDocumentByRef(ctx context.Context, documentRef string) (string, error) {
	documentRef = strings.TrimSpace(documentRef)
	if documentRef == "" {
		return "", nil
	}
	var exists string
	err := r.pool.QueryRow(ctx, `SELECT document_ref FROM ar_open_items WHERE document_ref = $1`, documentRef).Scan(&exists)
	if err == nil {
		return "ar", nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	err = r.pool.QueryRow(ctx, `SELECT document_ref FROM ap_open_items WHERE document_ref = $1`, documentRef).Scan(&exists)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return "ap", nil
}

var docRefPattern = regexp.MustCompile(`(?i)(INV|CN|DN|BILL|PO|PAY)[-_A-Z0-9]+`)

func extractDocumentRef(text string) string {
	m := docRefPattern.FindString(strings.ToUpper(strings.TrimSpace(text)))
	return m
}

// AutoMatchStatementLines attempts to match unmatched lines to payments or open documents.
func (r *Repository) AutoMatchStatementLines(ctx context.Context, statementID uuid.UUID) (int, error) {
	lines, err := r.ListStatementLines(ctx, statementID)
	if err != nil {
		return 0, err
	}
	matched := 0
	for _, l := range lines {
		if l.MatchStatus != "unmatched" {
			continue
		}
		candidate := strings.TrimSpace(l.ExternalRef)
		if candidate == "" {
			candidate = extractDocumentRef(l.Description)
		}
		if candidate == "" {
			candidate = extractDocumentRef(l.Payee)
		}
		if candidate == "" {
			continue
		}
		docRef, _, err := r.FindDocumentRefByPaymentRef(ctx, candidate)
		if err != nil {
			return matched, err
		}
		if docRef == "" {
			ledger, err := r.FindOpenDocumentByRef(ctx, candidate)
			if err != nil {
				return matched, err
			}
			if ledger != "" {
				docRef = candidate
			}
		}
		if docRef == "" {
			continue
		}
		if err := r.MatchStatementLine(ctx, l.ID, docRef); err != nil {
			if errors.Is(err, ErrStatementLineNotFound) {
				continue
			}
			return matched, err
		}
		matched++
	}
	if matched > 0 {
		code, err := r.BankAccountCodeForStatement(ctx, statementID)
		if err != nil {
			return matched, err
		}
		if err := r.MaterializeBankTransactions(ctx, code, statementID); err != nil {
			return matched, err
		}
		_, _ = r.pool.Exec(ctx, `UPDATE bank_statements SET status = 'reconciling', updated_at = NOW() WHERE id = $1`, statementID)
	}
	return matched, nil
}
