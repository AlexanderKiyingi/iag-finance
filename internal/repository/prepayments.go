package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// IAS 1 matching — prepaid-expense amortization (the expense-side mirror of the
// IFRS 15 deferral engine in revrec.go).
//
// Creating a schedule capitalises the outlay (Dr 1250 Prepaid / Cr funding) and
// lays out straight-line slices; an amortization run releases each due slice to
// expense (Dr <expense> / Cr 1250). Straight-line only.

const (
	prepaidCode        = "1250"
	defaultExpenseCode = "5100" // Operating Expenses
	defaultFundingCode = "1000" // Cash
)

var (
	// ErrPrepaymentExists indicates a schedule already covers this source ref.
	ErrPrepaymentExists = errors.New("prepayment schedule already exists for source")
	// ErrPrepaymentNotFound indicates the referenced schedule is absent.
	ErrPrepaymentNotFound = errors.New("prepayment schedule not found")
)

// PrepaidSchedule is a prepaid-expense amortization plan over one or more periods.
type PrepaidSchedule struct {
	ID          uuid.UUID       `json:"id"`
	SourceRef   string          `json:"sourceRef"`
	Total       decimal.Decimal `json:"total"`
	Currency    string          `json:"currency"`
	ExpenseCode string          `json:"expenseCode"`
	FundingCode string          `json:"fundingCode"`
	StartPeriod string          `json:"startPeriod"`
	Periods     int             `json:"periods"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// CreatePrepaymentInput describes a new prepaid-expense schedule.
type CreatePrepaymentInput struct {
	SourceRef   string
	Total       decimal.Decimal
	Currency    string
	ExpenseCode string // account amortized into (default 5100)
	FundingCode string // account credited at capitalization (default 1000)
	StartPeriod string // YYYY-MM
	Periods     int
}

// CreatePrepayment books the capitalization (Dr 1250 Prepaid / Cr funding) for
// the total and lays out the straight-line amortization plan. Idempotent on
// prepaid:<sourceRef>.
func (r *Repository) CreatePrepayment(ctx context.Context, in CreatePrepaymentInput, postedAt time.Time, audit *AuditInfo) (*PrepaidSchedule, error) {
	if in.Total.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("prepayment total must be positive")
	}
	if in.Currency == "" {
		in.Currency = r.baseCurrency
	}
	if in.ExpenseCode == "" {
		in.ExpenseCode = defaultExpenseCode
	}
	if in.FundingCode == "" {
		in.FundingCode = defaultFundingCode
	}
	if in.Periods < 1 {
		in.Periods = 1
	}
	if _, err := time.Parse("2006-01", in.StartPeriod); err != nil {
		return nil, fmt.Errorf("startPeriod must be YYYY-MM: %w", err)
	}

	prepaidID, err := r.accountIDByCode(ctx, prepaidCode)
	if err != nil {
		return nil, err
	}
	fundingID, err := r.accountIDByCode(ctx, in.FundingCode)
	if err != nil {
		return nil, err
	}
	// Fail early if the target expense account does not exist.
	if _, err := r.accountIDByCode(ctx, in.ExpenseCode); err != nil {
		return nil, err
	}

	lines := []ResolvedLine{
		{AccountID: prepaidID, Debit: in.Total, Memo: "Prepayment " + in.SourceRef, LineOrder: 0},
		{AccountID: fundingID, Credit: in.Total, Memo: "Fund prepayment " + in.SourceRef, LineOrder: 1},
	}
	rate := r.RateOrOne(ctx, in.Currency, postedAt)
	eventID := "prepaid:" + in.SourceRef
	src := "iag.finance"

	var out *PrepaidSchedule
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Capitalize prepayment " + in.SourceRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       in.Currency,
		FXRate:         rate,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.prepayment.capitalize", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		var schedID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO prepaid_schedules (source_ref, entity_id, total, currency, expense_code, funding_code, start_period, periods, capitalize_entry_id)
			VALUES ($1, $2, $3::numeric, $4, $5, $6, $7, $8, $9)
			RETURNING id
		`, in.SourceRef, EntityFromContext(ctx), in.Total.String(), in.Currency, in.ExpenseCode, in.FundingCode, in.StartPeriod, in.Periods, entryID).Scan(&schedID)
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrPrepaymentExists
			}
			return err
		}
		for i, slice := range ratableSlices(in.Total, in.Periods) {
			period := addMonths(in.StartPeriod, i)
			if _, err := tx.Exec(ctx, `
				INSERT INTO prepaid_schedule_lines (schedule_id, period, amount)
				VALUES ($1, $2, $3::numeric)
			`, schedID, period, slice.String()); err != nil {
				return err
			}
		}
		out = &PrepaidSchedule{
			ID: schedID, SourceRef: in.SourceRef, Total: in.Total, Currency: in.Currency,
			ExpenseCode: in.ExpenseCode, FundingCode: in.FundingCode,
			StartPeriod: in.StartPeriod, Periods: in.Periods, Status: "active",
		}
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		// Idempotent replay — return the existing schedule.
		return r.GetPrepaymentBySourceRef(ctx, in.SourceRef)
	}
	return out, nil
}

// RunAmortization expenses every straight-line slice due on/before the period
// that has not yet been amortized, in a single entry: one debit per distinct
// expense account and one 1250 credit for the total. Idempotent on
// prepaidamort:<period> for the batch.
func (r *Repository) RunAmortization(ctx context.Context, period string, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, decimal.Decimal, int, error) {
	if _, err := time.Parse("2006-01", period); err != nil {
		return nil, decimal.Zero, 0, fmt.Errorf("period must be YYYY-MM: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.amount::text, s.expense_code
		FROM prepaid_schedule_lines l JOIN prepaid_schedules s ON s.id = l.schedule_id
		WHERE l.period <= $1 AND l.journal_entry_id IS NULL AND l.amount > 0
		ORDER BY l.period, l.id
	`, period)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	var dueIDs []uuid.UUID
	byExpense := map[string]decimal.Decimal{}
	total := decimal.Zero
	for rows.Next() {
		var id uuid.UUID
		var amt, expenseCode string
		if err := rows.Scan(&id, &amt, &expenseCode); err != nil {
			rows.Close()
			return nil, decimal.Zero, 0, err
		}
		a, _ := decimal.NewFromString(amt)
		dueIDs = append(dueIDs, id)
		byExpense[expenseCode] = byExpense[expenseCode].Add(a)
		total = total.Add(a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, decimal.Zero, 0, err
	}
	if len(dueIDs) == 0 || total.LessThanOrEqual(decimal.Zero) {
		return nil, decimal.Zero, 0, nil
	}

	prepaidID, err := r.accountIDByCode(ctx, prepaidCode)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	// Deterministic line order: expense debits sorted by code, then the credit.
	var lines []ResolvedLine
	order := 0
	for _, code := range sortedKeys(byExpense) {
		expenseID, err := r.accountIDByCode(ctx, code)
		if err != nil {
			return nil, decimal.Zero, 0, err
		}
		lines = append(lines, ResolvedLine{AccountID: expenseID, Debit: byExpense[code], Memo: "Amortize prepayment " + period, LineOrder: order})
		order++
	}
	lines = append(lines, ResolvedLine{AccountID: prepaidID, Credit: total, Memo: "Prepayment amortized " + period, LineOrder: order})

	eventID := "prepaidamort:" + period
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Prepayment amortization " + period,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.prepayment.amortize", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		for _, id := range dueIDs {
			if _, err := tx.Exec(ctx, `
				UPDATE prepaid_schedule_lines SET journal_entry_id = $2, recognized_at = NOW()
				WHERE id = $1 AND journal_entry_id IS NULL
			`, id, entryID); err != nil {
				return err
			}
		}
		// Complete schedules whose lines are all amortized.
		_, err := tx.Exec(ctx, `
			UPDATE prepaid_schedules s SET status = 'completed'
			WHERE s.status = 'active'
			  AND NOT EXISTS (
				SELECT 1 FROM prepaid_schedule_lines l
				WHERE l.schedule_id = s.id AND l.journal_entry_id IS NULL
			  )
		`)
		return err
	}, audit)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	return entry, total, len(dueIDs), nil
}

// GetPrepaymentBySourceRef returns the schedule for a source ref.
func (r *Repository) GetPrepaymentBySourceRef(ctx context.Context, sourceRef string) (*PrepaidSchedule, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, source_ref, total::text, currency, expense_code, funding_code, start_period, periods, status, created_at
		FROM prepaid_schedules WHERE source_ref = $1
	`, sourceRef)
	return scanPrepayment(row)
}

// ListPrepayments returns recent schedules, newest first.
func (r *Repository) ListPrepayments(ctx context.Context, limit int) ([]PrepaidSchedule, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_ref, total::text, currency, expense_code, funding_code, start_period, periods, status, created_at
		FROM prepaid_schedules ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrepaidSchedule
	for rows.Next() {
		s, err := scanPrepayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func scanPrepayment(row scannable) (*PrepaidSchedule, error) {
	var s PrepaidSchedule
	var total string
	if err := row.Scan(&s.ID, &s.SourceRef, &total, &s.Currency, &s.ExpenseCode, &s.FundingCode, &s.StartPeriod, &s.Periods, &s.Status, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPrepaymentNotFound
		}
		return nil, err
	}
	s.Total, _ = decimal.NewFromString(total)
	return &s, nil
}

// sortedKeys returns the map keys in ascending order for deterministic output.
func sortedKeys(m map[string]decimal.Decimal) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
