package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type ResolvedLine struct {
	AccountID uuid.UUID
	Debit     decimal.Decimal
	Credit    decimal.Decimal
	Memo      string
	LineOrder int
}

type CreateJournalParams struct {
	EntryNumber   string
	Description   string
	Status        string
	SourceEventID *string
	SourceService *string
	CorrelationID *string
	CreatedBy     *uuid.UUID
	Lines         []ResolvedLine
}

var defaultAccounts = []struct {
	Code        string
	Name        string
	AccountType string
}{
	{"1000", "Cash", "asset"},
	{"1100", "Accounts Receivable", "asset"},
	{"2000", "Accounts Payable", "liability"},
	{"3000", "Retained Earnings", "equity"},
	{"4000", "Sales Revenue", "revenue"},
	{"5000", "Cost of Goods Sold", "expense"},
	{"5100", "Operating Expenses", "expense"},
	{"2100", "VAT Payable", "liability"},
}

func (r *Repository) SeedChartOfAccounts(ctx context.Context) error {
	for _, a := range defaultAccounts {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO chart_of_accounts (code, name, account_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (code) DO NOTHING
		`, a.Code, a.Name, a.AccountType)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListChartOfAccounts(ctx context.Context) ([]domain.ChartAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, account_type, parent_id, currency, active, created_at, updated_at
		FROM chart_of_accounts
		WHERE active = TRUE
		ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.ChartAccount
	for rows.Next() {
		var a domain.ChartAccount
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (r *Repository) CreateChartAccount(ctx context.Context, code, name, accountType, currency string, parentID *uuid.UUID) (*domain.ChartAccount, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO chart_of_accounts (code, name, account_type, currency, parent_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, code, name, account_type, parent_id, currency, active, created_at, updated_at
	`, code, name, accountType, currency, parentID)

	var a domain.ChartAccount
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) GetAccountByCode(ctx context.Context, code string) (*domain.ChartAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, code, name, account_type, parent_id, currency, active, created_at, updated_at
		FROM chart_of_accounts WHERE code = $1 AND active = TRUE
	`, code)

	var a domain.ChartAccount
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *Repository) NextEntryNumber(ctx context.Context) (string, error) {
	var entryNumber string
	err := r.pool.QueryRow(ctx, `
		SELECT 'JE-' || LPAD(nextval('journal_entry_number_seq')::text, 6, '0')
	`).Scan(&entryNumber)
	if err != nil {
		return "", err
	}
	return entryNumber, nil
}

func (r *Repository) CreateJournalEntry(ctx context.Context, p CreateJournalParams) (*domain.JournalEntry, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var entry domain.JournalEntry
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries (
			entry_number, description, status, source_event_id, source_service, correlation_id, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, entry_number, description, status, source_event_id, source_service, correlation_id, posted_at, created_by, created_at, updated_at
	`, p.EntryNumber, p.Description, p.Status, p.SourceEventID, p.SourceService, p.CorrelationID, p.CreatedBy).Scan(
		&entry.ID, &entry.EntryNumber, &entry.Description, &entry.Status,
		&entry.SourceEventID, &entry.SourceService, &entry.CorrelationID,
		&entry.PostedAt, &entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	for _, line := range p.Lines {
		var jl domain.JournalLine
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_lines (journal_entry_id, account_id, debit, credit, memo, line_order)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, journal_entry_id, account_id, debit, credit, memo, line_order
		`, entry.ID, line.AccountID, line.Debit, line.Credit, line.Memo, line.LineOrder).Scan(
			&jl.ID, &jl.JournalEntryID, &jl.AccountID, &jl.Debit, &jl.Credit, &jl.Memo, &jl.LineOrder,
		)
		if err != nil {
			return nil, err
		}
		entry.Lines = append(entry.Lines, jl)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *Repository) ListJournalEntries(ctx context.Context, limit, offset int) ([]domain.JournalEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id, posted_at, created_by, created_at, updated_at
		FROM journal_entries
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.JournalEntry
	for rows.Next() {
		var e domain.JournalEntry
		if err := rows.Scan(
			&e.ID, &e.EntryNumber, &e.Description, &e.Status,
			&e.SourceEventID, &e.SourceService, &e.CorrelationID,
			&e.PostedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func (r *Repository) GetJournalEntry(ctx context.Context, id uuid.UUID) (*domain.JournalEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id, posted_at, created_by, created_at, updated_at
		FROM journal_entries WHERE id = $1
	`, id)

	var e domain.JournalEntry
	if err := row.Scan(
		&e.ID, &e.EntryNumber, &e.Description, &e.Status,
		&e.SourceEventID, &e.SourceService, &e.CorrelationID,
		&e.PostedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	lines, err := r.loadJournalLines(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Lines = lines
	return &e, nil
}

func (r *Repository) GetJournalEntryBySourceEvent(ctx context.Context, eventID string) (*domain.JournalEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id, posted_at, created_by, created_at, updated_at
		FROM journal_entries WHERE source_event_id = $1
	`, eventID)

	var e domain.JournalEntry
	if err := row.Scan(
		&e.ID, &e.EntryNumber, &e.Description, &e.Status,
		&e.SourceEventID, &e.SourceService, &e.CorrelationID,
		&e.PostedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	lines, err := r.loadJournalLines(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	e.Lines = lines
	return &e, nil
}

func (r *Repository) loadJournalLines(ctx context.Context, entryID uuid.UUID) ([]domain.JournalLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT jl.id, jl.journal_entry_id, jl.account_id, coa.code, coa.name, jl.debit, jl.credit, jl.memo, jl.line_order
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		WHERE jl.journal_entry_id = $1
		ORDER BY jl.line_order
	`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []domain.JournalLine
	for rows.Next() {
		var l domain.JournalLine
		if err := rows.Scan(&l.ID, &l.JournalEntryID, &l.AccountID, &l.AccountCode, &l.AccountName, &l.Debit, &l.Credit, &l.Memo, &l.LineOrder); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

func (r *Repository) UpdateJournalStatus(ctx context.Context, id uuid.UUID, status string, postedAt time.Time) (*domain.JournalEntry, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE journal_entries SET status = $2, posted_at = $3, updated_at = NOW() WHERE id = $1
	`, id, status, postedAt)
	if err != nil {
		return nil, err
	}
	return r.GetJournalEntry(ctx, id)
}

func (r *Repository) IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`, eventID).Scan(&exists)
	return exists, err
}

func (r *Repository) MarkEventProcessed(ctx context.Context, eventID, eventType string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, eventType)
	return err
}

func (r *Repository) ListAROpenItems(ctx context.Context, limit, offset int) ([]domain.AROpenItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_ref, document_ref, description, amount::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ar_open_items
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanARItems(rows)
}

func (r *Repository) ListAPOpenItems(ctx context.Context, limit, offset int) ([]domain.APOpenItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, vendor_ref, document_ref, description, amount::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
		FROM ap_open_items
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPItems(rows)
}

func scanARItems(rows pgx.Rows) ([]domain.AROpenItem, error) {
	var items []domain.AROpenItem
	for rows.Next() {
		var i domain.AROpenItem
		if err := rows.Scan(&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func scanAPItems(rows pgx.Rows) ([]domain.APOpenItem, error) {
	var items []domain.APOpenItem
	for rows.Next() {
		var i domain.APOpenItem
		if err := rows.Scan(&i.ID, &i.VendorRef, &i.DocumentRef, &i.Description, &i.Amount, &i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (r *Repository) CreateAROpenItem(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, journalEntryID *uuid.UUID, sourceEventID *string) (*domain.AROpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ar_open_items (customer_ref, document_ref, description, amount, currency, due_date, journal_entry_id, source_event_id)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8)
		RETURNING id, customer_ref, document_ref, description, amount::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
	`, customerRef, documentRef, description, amount, currency, dueDate, journalEntryID, sourceEventID)

	var i domain.AROpenItem
	if err := row.Scan(&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *Repository) CreateAPOpenItem(ctx context.Context, vendorRef, documentRef, description, amount, currency string, dueDate *time.Time, journalEntryID *uuid.UUID, sourceEventID *string) (*domain.APOpenItem, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ap_open_items (vendor_ref, document_ref, description, amount, currency, due_date, journal_entry_id, source_event_id)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8)
		RETURNING id, vendor_ref, document_ref, description, amount::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
	`, vendorRef, documentRef, description, amount, currency, dueDate, journalEntryID, sourceEventID)

	var i domain.APOpenItem
	if err := row.Scan(&i.ID, &i.VendorRef, &i.DocumentRef, &i.Description, &i.Amount, &i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

type TrialBalanceRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
}

func (r *Repository) TrialBalance(ctx context.Context) ([]TrialBalanceRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name,
			COALESCE(SUM(jl.debit), 0)::text,
			COALESCE(SUM(jl.credit), 0)::text
		FROM chart_of_accounts coa
		LEFT JOIN journal_lines jl ON jl.account_id = coa.id
		LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
		WHERE coa.active = TRUE
		GROUP BY coa.id, coa.code, coa.name
		ORDER BY coa.code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrialBalanceRow
	for rows.Next() {
		var row TrialBalanceRow
		if err := rows.Scan(&row.AccountCode, &row.AccountName, &row.Debit, &row.Credit); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
