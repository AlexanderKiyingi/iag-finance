package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// ErrPeriodClosed indicates a post was attempted into a closed fiscal period.
var ErrPeriodClosed = errors.New("accounting period is closed")

type Repository struct {
	pool         *pgxpool.Pool
	baseCurrency string
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, baseCurrency: "UGX"}
}

// SetBaseCurrency sets the reporting/base currency (default UGX). All journal
// lines store a base-currency equivalent computed against it.
func (r *Repository) SetBaseCurrency(c string) {
	if c != "" {
		r.baseCurrency = c
	}
}

// BaseCurrency returns the configured base/reporting currency.
func (r *Repository) BaseCurrency() string { return r.baseCurrency }

type ResolvedLine struct {
	AccountID uuid.UUID
	Debit     decimal.Decimal
	Credit    decimal.Decimal
	Memo      string
	LineOrder int
	// Currency overrides the entry currency for this line (""=entry currency).
	// Used by multi-rate entries (e.g. realized FX, where a base-currency
	// gain/loss line sits alongside foreign-currency legs).
	Currency string
	// DebitBase/CreditBase, when non-nil, set this line's base-currency amount
	// explicitly instead of computing nominal × the entry rate. Lets one entry
	// carry legs converted at different rates (the basis of FX gain/loss).
	DebitBase  *decimal.Decimal
	CreditBase *decimal.Decimal
	// Optional job-costing dimensions (Phase 6).
	CostCenterID *uuid.UUID
	ProjectID    *uuid.UUID
}

// baseAmounts returns the line's (debitBase, creditBase): explicit overrides when
// set, otherwise nominal × the supplied entry rate.
func (l ResolvedLine) baseAmounts(rate decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	db := l.Debit.Mul(rate).Round(2)
	if l.DebitBase != nil {
		db = l.DebitBase.Round(2)
	}
	cr := l.Credit.Mul(rate).Round(2)
	if l.CreditBase != nil {
		cr = l.CreditBase.Round(2)
	}
	return db, cr
}

// currencyOr returns the line currency or the entry default.
func (l ResolvedLine) currencyOr(entryCurrency string) string {
	if l.Currency != "" {
		return l.Currency
	}
	return entryCurrency
}

type CreateJournalParams struct {
	EntryNumber    string
	Description    string
	Status         string
	SourceEventID  *string
	SourceService  *string
	CorrelationID  *string
	CreatedBy      *uuid.UUID
	AccountingDate time.Time       // zero → defaults to the posting date / today
	Currency       string          // transaction currency of the lines (zero → base)
	FXRate         decimal.Decimal // currency→base rate (zero → 1)
	Lines          []ResolvedLine
}

// lineCurrency returns the params' transaction currency, defaulting to base.
func (p CreateJournalParams) lineCurrency(base string) string {
	if p.Currency == "" {
		return base
	}
	return p.Currency
}

// rate returns the params' FX rate, defaulting to 1 (base currency / no FX).
func (p CreateJournalParams) rate() decimal.Decimal {
	if p.FXRate.IsZero() {
		return decimal.NewFromInt(1)
	}
	return p.FXRate
}

// resolveAccountingDate returns the params' accounting date, or fallback when
// unset (zero). Keeps every booking path consistent.
func resolveAccountingDate(d, fallback time.Time) time.Time {
	if d.IsZero() {
		return fallback
	}
	return d
}

var defaultAccounts = []struct {
	Code        string
	Name        string
	AccountType string
	IsCash      bool
}{
	{"1000", "Cash", "asset", true},
	{"1100", "Accounts Receivable", "asset", false},
	{"2000", "Accounts Payable", "liability", false},
	{"3000", "Retained Earnings", "equity", false},
	{"4000", "Sales Revenue", "revenue", false},
	{"5000", "Cost of Goods Sold", "expense", false},
	{"5100", "Operating Expenses", "expense", false},
	{"2100", "VAT Payable", "liability", false},
	// IFRS 9 — expected-credit-loss allowance & bad-debt accounts (migration 039).
	{"1190", "Allowance for Doubtful Debts", "asset", false},
	{"5400", "Bad Debt Expense", "expense", false},
	{"4300", "Bad Debt Recovery", "revenue", false},
	// IFRS 15 — deferred & accrued revenue (migration 040).
	{"2300", "Deferred Revenue", "liability", false},
	{"1200", "Accrued Revenue", "asset", false},
	// IAS 1 matching — prepaid-expense amortization (migration 052).
	{"1250", "Prepaid Expenses", "asset", false},
	// IFRS 16 — leases (migration 053).
	{"1600", "Right-of-Use Assets", "asset", false},
	{"1610", "Accumulated Depreciation - ROU", "asset", false},
	{"2500", "Lease Liability", "liability", false},
	{"5320", "Depreciation - Right-of-Use Assets", "expense", false},
	{"5600", "Interest Expense - Leases", "expense", false},
	// IAS 16 / IAS 36 — impairment & revaluation (migration 041).
	{"5310", "Impairment Loss", "expense", false},
	{"3100", "Revaluation Surplus", "equity", false},
	// IAS 37 — provisions & decommissioning (migration 042).
	{"2400", "Provisions", "liability", false},
	{"2410", "Decommissioning Provision", "liability", false},
	{"5500", "Provision Expense", "expense", false},
	{"5510", "Finance Cost - Unwinding of Discount", "expense", false},
	// Three-way match — purchase price variance (migration 043).
	{"5150", "Purchase Price Variance", "expense", false},
}

func (r *Repository) SeedChartOfAccounts(ctx context.Context) error {
	for _, a := range defaultAccounts {
		// Set is_cash_equivalent on conflict too, so the cash-flow flag is correct
		// on a fresh database regardless of seed/migration order (the name is left
		// untouched to preserve any operator rename).
		_, err := r.pool.Exec(ctx, `
			INSERT INTO chart_of_accounts (code, name, account_type, is_cash_equivalent)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (code) DO UPDATE SET is_cash_equivalent = EXCLUDED.is_cash_equivalent
		`, a.Code, a.Name, a.AccountType, a.IsCash)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListChartOfAccounts(ctx context.Context) ([]domain.ChartAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, account_type, parent_id, currency, active, restrict_to_natural_side, created_at, updated_at
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
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.RestrictToNaturalSide, &a.CreatedAt, &a.UpdatedAt); err != nil {
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
		RETURNING id, code, name, account_type, parent_id, currency, active, restrict_to_natural_side, created_at, updated_at
	`, code, name, accountType, currency, parentID)

	var a domain.ChartAccount
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.RestrictToNaturalSide, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateChartAccount applies a partial update. Each nullable argument is left
// unchanged when nil (COALESCE), so callers send only the fields they touch.
// `code` is intentionally immutable — it is the human-facing identity and is
// referenced by code elsewhere (e.g. budgets). Returns (nil, nil) when no row
// with the id exists.
func (r *Repository) UpdateChartAccount(ctx context.Context, id uuid.UUID, name, accountType, currency *string, parentID *uuid.UUID, active, restrictNaturalSide *bool) (*domain.ChartAccount, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE chart_of_accounts SET
			name = COALESCE($2, name),
			account_type = COALESCE($3, account_type),
			currency = COALESCE($4, currency),
			parent_id = COALESCE($5, parent_id),
			active = COALESCE($6, active),
			restrict_to_natural_side = COALESCE($7, restrict_to_natural_side),
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, code, name, account_type, parent_id, currency, active, restrict_to_natural_side, created_at, updated_at
	`, id, name, accountType, currency, parentID, active, restrictNaturalSide)

	var a domain.ChartAccount
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.RestrictToNaturalSide, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// DeactivateChartAccount soft-deletes an account by clearing its active flag.
// A hard delete is unsafe: journal_lines.account_id has no ON DELETE rule, so
// any account that has ever been posted to cannot be removed. Deactivation
// hides it from the (active-only) list without breaking historical postings.
// Returns false when no active row with the id exists (already gone / unknown).
func (r *Repository) DeactivateChartAccount(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE chart_of_accounts SET active = FALSE, updated_at = NOW()
		WHERE id = $1 AND active = TRUE
	`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) GetAccountByCode(ctx context.Context, code string) (*domain.ChartAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, code, name, account_type, parent_id, currency, active, restrict_to_natural_side, created_at, updated_at
		FROM chart_of_accounts WHERE code = $1 AND active = TRUE
	`, code)

	var a domain.ChartAccount
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.ParentID, &a.Currency, &a.Active, &a.RestrictToNaturalSide, &a.CreatedAt, &a.UpdatedAt); err != nil {
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
			entry_number, description, status, source_event_id, source_service, correlation_id, created_by, accounting_date, entity_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, entry_number, description, status, source_event_id, source_service, correlation_id, posted_at, created_by, created_at, updated_at
	`, p.EntryNumber, p.Description, p.Status, p.SourceEventID, p.SourceService, p.CorrelationID, p.CreatedBy, resolveAccountingDate(p.AccountingDate, time.Now().UTC()), EntityFromContext(ctx)).Scan(
		&entry.ID, &entry.EntryNumber, &entry.Description, &entry.Status,
		&entry.SourceEventID, &entry.SourceService, &entry.CorrelationID,
		&entry.PostedAt, &entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	entryCurrency := p.lineCurrency(r.baseCurrency)
	rate := p.rate()
	for _, line := range p.Lines {
		var jl domain.JournalLine
		debitBase, creditBase := line.baseAmounts(rate)
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_lines (journal_entry_id, account_id, debit, credit, memo, line_order, currency, debit_base, credit_base, cost_center_id, project_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, journal_entry_id, account_id, debit, credit, memo, line_order
		`, entry.ID, line.AccountID, line.Debit, line.Credit, line.Memo, line.LineOrder, line.currencyOr(entryCurrency), debitBase, creditBase, line.CostCenterID, line.ProjectID).Scan(
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
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id,
		       to_char(accounting_date,'YYYY-MM-DD'), reverses_entry_id, posted_at, created_by, created_at, updated_at
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
			&e.AccountingDate, &e.ReversesEntryID,
			&e.PostedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Attach lines so clients can render amounts and the debit/credit accounts
	// without an N+1 fetch per entry.
	ids := make([]uuid.UUID, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	byEntry, err := r.loadJournalLinesForEntries(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Lines = byEntry[items[i].ID]
	}
	return items, nil
}

func (r *Repository) GetJournalEntry(ctx context.Context, id uuid.UUID) (*domain.JournalEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id,
		       to_char(accounting_date,'YYYY-MM-DD'), reverses_entry_id, posted_at, created_by, created_at, updated_at
		FROM journal_entries WHERE id = $1
	`, id)

	var e domain.JournalEntry
	if err := row.Scan(
		&e.ID, &e.EntryNumber, &e.Description, &e.Status,
		&e.SourceEventID, &e.SourceService, &e.CorrelationID,
		&e.AccountingDate, &e.ReversesEntryID,
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
		SELECT id, entry_number, description, status, source_event_id, source_service, correlation_id,
		       to_char(accounting_date,'YYYY-MM-DD'), reverses_entry_id, posted_at, created_by, created_at, updated_at
		FROM journal_entries WHERE source_event_id = $1
	`, eventID)

	var e domain.JournalEntry
	if err := row.Scan(
		&e.ID, &e.EntryNumber, &e.Description, &e.Status,
		&e.SourceEventID, &e.SourceService, &e.CorrelationID,
		&e.AccountingDate, &e.ReversesEntryID,
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

// loadJournalLinesForEntries batch-loads lines for many entries in one query,
// grouped by entry id (avoids N+1 when listing). Returns an empty map for no ids.
func (r *Repository) loadJournalLinesForEntries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.JournalLine, error) {
	byEntry := make(map[uuid.UUID][]domain.JournalLine, len(ids))
	if len(ids) == 0 {
		return byEntry, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT jl.id, jl.journal_entry_id, jl.account_id, coa.code, coa.name, jl.debit, jl.credit, jl.memo, jl.line_order
		FROM journal_lines jl
		JOIN chart_of_accounts coa ON coa.id = jl.account_id
		WHERE jl.journal_entry_id = ANY($1)
		ORDER BY jl.journal_entry_id, jl.line_order
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.JournalLine
		if err := rows.Scan(&l.ID, &l.JournalEntryID, &l.AccountID, &l.AccountCode, &l.AccountName, &l.Debit, &l.Credit, &l.Memo, &l.LineOrder); err != nil {
			return nil, err
		}
		byEntry[l.JournalEntryID] = append(byEntry[l.JournalEntryID], l)
	}
	return byEntry, rows.Err()
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

// MarkEntryPosted flips a draft entry to posted, but only while it is still
// draft. The WHERE status='draft' guard is the authoritative concurrency
// control: two racing posts serialize on the row, and the loser matches zero
// rows (status is already 'posted') rather than double-posting. The fiscal
// period is checked against the entry's own accounting_date (not wall-clock),
// and the audit chain entry is appended in the same transaction. Returns false
// when no draft row matched (already posted, reversed, or missing);
// ErrPeriodClosed when the entry's accounting period is closed.
func (r *Repository) MarkEntryPosted(ctx context.Context, id uuid.UUID, postedAt time.Time, audit *AuditInfo) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var status, period string
	err = tx.QueryRow(ctx, `
		SELECT status, to_char(accounting_date, 'YYYY-MM') FROM journal_entries WHERE id = $1 FOR UPDATE
	`, id).Scan(&status, &period)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if status != "draft" {
		return false, nil
	}

	var periodStatus string
	switch err := tx.QueryRow(ctx, `SELECT status FROM fiscal_periods WHERE period = $1`, period).Scan(&periodStatus); {
	case err == nil:
		if periodStatus == "closed" {
			return false, ErrPeriodClosed
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No row → period open by default.
	default:
		return false, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE journal_entries SET status = 'posted', posted_at = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'draft'
	`, id, postedAt)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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
		SELECT id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
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
		SELECT id, vendor_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, party_id, created_at, updated_at
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

func (r *Repository) ListAPByPartyID(ctx context.Context, partyID uuid.UUID, limit, offset int) ([]domain.APOpenItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, vendor_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, party_id, created_at, updated_at
		FROM ap_open_items
		WHERE party_id = $1
		ORDER BY due_date DESC NULLS LAST
		LIMIT $2 OFFSET $3
	`, partyID, limit, offset)
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
		if err := rows.Scan(
			&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
			&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID,
			&i.CreatedAt, &i.UpdatedAt,
		); err != nil {
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
		if err := rows.Scan(
			&i.ID, &i.VendorRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
			&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.PartyID,
			&i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// CreateAROpenItem inserts an AR open item and, when outbox is non-nil, enqueues
// its domain event in the same transaction so the two can never diverge.
func (r *Repository) CreateAROpenItem(ctx context.Context, customerRef, documentRef, description, amount, currency string, dueDate *time.Time, journalEntryID *uuid.UUID, sourceEventID *string, outbox *OutboxEvent) (*domain.AROpenItem, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	fxRate := r.RateOrOne(ctx, currency, time.Now().UTC())
	var i domain.AROpenItem
	if err := tx.QueryRow(ctx, `
		INSERT INTO ar_open_items (customer_ref, document_ref, description, amount, currency, due_date, journal_entry_id, source_event_id, fx_rate, entity_id)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9::numeric, $10)
		RETURNING id, customer_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, created_at, updated_at
	`, customerRef, documentRef, description, amount, currency, dueDate, journalEntryID, sourceEventID, fxRate.String(), EntityFromContext(ctx)).Scan(
		&i.ID, &i.CustomerRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
		&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if outbox != nil {
		if err := enqueueOutboxTx(ctx, tx, *outbox); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &i, nil
}

// CreateAPOpenItem inserts an AP open item and, when outbox is non-nil, enqueues
// its domain event in the same transaction.
func (r *Repository) CreateAPOpenItem(ctx context.Context, vendorRef, documentRef, description, amount, currency string, dueDate *time.Time, journalEntryID *uuid.UUID, sourceEventID *string, outbox *OutboxEvent) (*domain.APOpenItem, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	fxRate := r.RateOrOne(ctx, currency, time.Now().UTC())
	var i domain.APOpenItem
	if err := tx.QueryRow(ctx, `
		INSERT INTO ap_open_items (vendor_ref, document_ref, description, amount, currency, due_date, journal_entry_id, source_event_id, fx_rate, entity_id)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9::numeric, $10)
		RETURNING id, vendor_ref, document_ref, description, amount::text, amount_paid::text, currency, due_date, status, journal_entry_id, source_event_id, party_id, created_at, updated_at
	`, vendorRef, documentRef, description, amount, currency, dueDate, journalEntryID, sourceEventID, fxRate.String(), EntityFromContext(ctx)).Scan(
		&i.ID, &i.VendorRef, &i.DocumentRef, &i.Description, &i.Amount, &i.AmountPaid,
		&i.Currency, &i.DueDate, &i.Status, &i.JournalEntryID, &i.SourceEventID, &i.PartyID,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if outbox != nil {
		if err := enqueueOutboxTx(ctx, tx, *outbox); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
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

// TrialBalance sums posted debits/credits per account, optionally bounded to a
// [from, to] accounting-date range (nil = unbounded on that side).
func (r *Repository) TrialBalance(ctx context.Context, from, to *time.Time, entityIDs []uuid.UUID) ([]TrialBalanceRow, error) {
	// FILTER the SUMs to rows where the posted + in-range journal-entry join
	// matched (je.id IS NOT NULL). This keeps every active account on the trial
	// balance (zero-activity accounts show 0 / 0) while ensuring draft and
	// out-of-range lines never inflate the totals — otherwise the LEFT JOIN would
	// sum every line regardless of status/date, making the bounds a no-op.
	rows, err := r.pool.Query(ctx, `
		SELECT coa.code, coa.name,
			COALESCE(SUM(jl.debit_base) FILTER (WHERE je.id IS NOT NULL), 0)::text,
			COALESCE(SUM(jl.credit_base) FILTER (WHERE je.id IS NOT NULL), 0)::text
		FROM chart_of_accounts coa
		LEFT JOIN journal_lines jl ON jl.account_id = coa.id
		LEFT JOIN journal_entries je ON je.id = jl.journal_entry_id AND je.status = 'posted'
			AND ($1::date IS NULL OR je.accounting_date >= $1)
			AND ($2::date IS NULL OR je.accounting_date <= $2)
			AND je.entity_id = ANY($3::uuid[])
		WHERE coa.active = TRUE
		GROUP BY coa.id, coa.code, coa.name
		ORDER BY coa.code
	`, from, to, entityIDs)
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
