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

// IFRS 15 revenue recognition: deferral, scheduled recognition, accrual.
//
// A schedule reclassifies revenue already recognised at invoice issue into
// Deferred Revenue (Dr 4000 / Cr 2300) and spreads it over periods; a recognition
// run releases each due slice back to revenue (Dr 2300 / Cr 4000). Accrued
// revenue is the mirror for work done ahead of billing (Dr 1200 / Cr 4000), later
// billed (Dr 1100 / Cr 1200).

const (
	revenueCode  = "4000"
	deferredCode = "2300"
	accruedCode  = "1200"
)

var (
	// ErrScheduleExists indicates a schedule already covers this source ref.
	ErrScheduleExists = errors.New("revenue schedule already exists for source")
	// ErrScheduleNotFound indicates the referenced schedule/obligation is absent.
	ErrScheduleNotFound = errors.New("revenue schedule not found")
	// ErrObligationSatisfied indicates the obligation was already recognised.
	ErrObligationSatisfied = errors.New("performance obligation already satisfied")
)

// RevenueSchedule is a deferral plan over one or more periods.
type RevenueSchedule struct {
	ID          uuid.UUID       `json:"id"`
	SourceRef   string          `json:"sourceRef"`
	Total       decimal.Decimal `json:"total"`
	Currency    string          `json:"currency"`
	Method      string          `json:"method"`
	StartPeriod string          `json:"startPeriod"`
	Periods     int             `json:"periods"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// CreateScheduleInput describes a new recognition schedule.
type CreateScheduleInput struct {
	SourceRef   string
	Total       decimal.Decimal
	Currency    string
	Method      string // ratable | milestone
	StartPeriod string // YYYY-MM
	Periods     int
	Obligations []ObligationInput // for method=milestone
}

// ObligationInput is one milestone performance obligation.
type ObligationInput struct {
	Description string
	Amount      decimal.Decimal
}

// CreateSchedule books the deferral (Dr Revenue / Cr Deferred Revenue) for the
// total and lays out the recognition plan — ratable slices or milestone
// obligations. Idempotent on revsched:<sourceRef>.
func (r *Repository) CreateSchedule(ctx context.Context, in CreateScheduleInput, postedAt time.Time, audit *AuditInfo) (*RevenueSchedule, error) {
	if in.Total.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("schedule total must be positive")
	}
	if in.Method == "" {
		in.Method = "ratable"
	}
	if in.Currency == "" {
		in.Currency = r.baseCurrency
	}
	if in.Periods < 1 {
		in.Periods = 1
	}
	if _, err := time.Parse("2006-01", in.StartPeriod); err != nil {
		return nil, fmt.Errorf("startPeriod must be YYYY-MM: %w", err)
	}

	revenueID, err := r.accountIDByCode(ctx, revenueCode)
	if err != nil {
		return nil, err
	}
	deferredID, err := r.accountIDByCode(ctx, deferredCode)
	if err != nil {
		return nil, err
	}

	lines := []ResolvedLine{
		{AccountID: revenueID, Debit: in.Total, Memo: "Defer revenue " + in.SourceRef, LineOrder: 0},
		{AccountID: deferredID, Credit: in.Total, Memo: "Deferred revenue " + in.SourceRef, LineOrder: 1},
	}
	rate := r.RateOrOne(ctx, in.Currency, postedAt)
	eventID := "revsched:" + in.SourceRef
	src := "iag.finance"

	var out *RevenueSchedule
	_, err = r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Defer revenue " + in.SourceRef,
		SourceEventID:  &eventID,
		SourceService:  &src,
		Currency:       in.Currency,
		FXRate:         rate,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.revenue.defer", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		var schedID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO revenue_schedules (source_ref, entity_id, total, currency, method, start_period, periods, defer_entry_id)
			VALUES ($1, $2, $3::numeric, $4, $5, $6, $7, $8)
			RETURNING id
		`, in.SourceRef, EntityFromContext(ctx), in.Total.String(), in.Currency, in.Method, in.StartPeriod, in.Periods, entryID).Scan(&schedID)
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrScheduleExists
			}
			return err
		}
		if in.Method == "milestone" {
			for _, o := range in.Obligations {
				if _, err := tx.Exec(ctx, `
					INSERT INTO performance_obligations (schedule_id, description, amount)
					VALUES ($1, $2, $3::numeric)
				`, schedID, o.Description, o.Amount.String()); err != nil {
					return err
				}
			}
		} else {
			for i, slice := range ratableSlices(in.Total, in.Periods) {
				period := addMonths(in.StartPeriod, i)
				if _, err := tx.Exec(ctx, `
					INSERT INTO revenue_schedule_lines (schedule_id, period, amount)
					VALUES ($1, $2, $3::numeric)
				`, schedID, period, slice.String()); err != nil {
					return err
				}
			}
		}
		out = &RevenueSchedule{
			ID: schedID, SourceRef: in.SourceRef, Total: in.Total, Currency: in.Currency,
			Method: in.Method, StartPeriod: in.StartPeriod, Periods: in.Periods, Status: "active",
		}
		return nil
	}, audit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		// Idempotent replay — return the existing schedule.
		return r.GetScheduleBySourceRef(ctx, in.SourceRef)
	}
	return out, nil
}

// RunRecognition releases every ratable slice due on/before the period that has
// not yet been recognised, in a single Dr Deferred / Cr Revenue entry, and
// stamps each line. Idempotent on revrec:<period> for the batch.
func (r *Repository) RunRecognition(ctx context.Context, period string, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, decimal.Decimal, int, error) {
	if _, err := time.Parse("2006-01", period); err != nil {
		return nil, decimal.Zero, 0, fmt.Errorf("period must be YYYY-MM: %w", err)
	}
	// Collect due, unrecognised lines.
	rows, err := r.pool.Query(ctx, `
		SELECT id, amount::text FROM revenue_schedule_lines
		WHERE period <= $1 AND journal_entry_id IS NULL AND amount > 0
		ORDER BY period, id
	`, period)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	type due struct {
		id     uuid.UUID
		amount decimal.Decimal
	}
	var dues []due
	total := decimal.Zero
	for rows.Next() {
		var d due
		var amt string
		if err := rows.Scan(&d.id, &amt); err != nil {
			rows.Close()
			return nil, decimal.Zero, 0, err
		}
		d.amount, _ = decimal.NewFromString(amt)
		dues = append(dues, d)
		total = total.Add(d.amount)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, decimal.Zero, 0, err
	}
	if len(dues) == 0 || total.LessThanOrEqual(decimal.Zero) {
		return nil, decimal.Zero, 0, nil
	}

	deferredID, err := r.accountIDByCode(ctx, deferredCode)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	revenueID, err := r.accountIDByCode(ctx, revenueCode)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	lines := []ResolvedLine{
		{AccountID: deferredID, Debit: total, Memo: "Recognise revenue " + period, LineOrder: 0},
		{AccountID: revenueID, Credit: total, Memo: "Revenue recognised " + period, LineOrder: 1},
	}
	eventID := "revrec:" + period
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Revenue recognition " + period,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.revenue.recognize", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		for _, d := range dues {
			if _, err := tx.Exec(ctx, `
				UPDATE revenue_schedule_lines SET journal_entry_id = $2, recognized_at = NOW()
				WHERE id = $1 AND journal_entry_id IS NULL
			`, d.id, entryID); err != nil {
				return err
			}
		}
		// Complete schedules whose lines are all recognised.
		_, err := tx.Exec(ctx, `
			UPDATE revenue_schedules s SET status = 'completed'
			WHERE s.status = 'active' AND s.method = 'ratable'
			  AND NOT EXISTS (
				SELECT 1 FROM revenue_schedule_lines l
				WHERE l.schedule_id = s.id AND l.journal_entry_id IS NULL
			  )
		`)
		return err
	}, audit)
	if err != nil {
		return nil, decimal.Zero, 0, err
	}
	return entry, total, len(dues), nil
}

// SatisfyObligation recognises one milestone obligation (Dr Deferred / Cr Revenue).
func (r *Repository) SatisfyObligation(ctx context.Context, obligationID uuid.UUID, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, error) {
	var amtS, status string
	var satisfied *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT o.amount::text, o.satisfied_at, s.currency
		FROM performance_obligations o JOIN revenue_schedules s ON s.id = o.schedule_id
		WHERE o.id = $1
	`, obligationID).Scan(&amtS, &satisfied, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrScheduleNotFound
	}
	if err != nil {
		return nil, err
	}
	if satisfied != nil {
		return nil, ErrObligationSatisfied
	}
	amount, _ := decimal.NewFromString(amtS)
	deferredID, err := r.accountIDByCode(ctx, deferredCode)
	if err != nil {
		return nil, err
	}
	revenueID, err := r.accountIDByCode(ctx, revenueCode)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: deferredID, Debit: amount, Memo: "Recognise obligation", LineOrder: 0},
		{AccountID: revenueID, Credit: amount, Memo: "Revenue recognised (milestone)", LineOrder: 1},
	}
	eventID := "revrec.obligation:" + obligationID.String()
	src := "iag.finance"
	entry, err := r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Milestone recognition",
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.revenue.milestone", postedAt, func(ctx context.Context, tx pgx.Tx, entryID uuid.UUID) error {
		_, err := tx.Exec(ctx, `
			UPDATE performance_obligations SET satisfied_at = NOW(), journal_entry_id = $2
			WHERE id = $1 AND satisfied_at IS NULL
		`, obligationID, entryID)
		return err
	}, audit)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// AccrueRevenue recognises revenue earned before billing (Dr Accrued / Cr Revenue).
func (r *Repository) AccrueRevenue(ctx context.Context, ref string, amount decimal.Decimal, postedAt time.Time, audit *AuditInfo) (*domain.JournalEntry, error) {
	accruedID, err := r.accountIDByCode(ctx, accruedCode)
	if err != nil {
		return nil, err
	}
	revenueID, err := r.accountIDByCode(ctx, revenueCode)
	if err != nil {
		return nil, err
	}
	lines := []ResolvedLine{
		{AccountID: accruedID, Debit: amount, Memo: "Accrued revenue " + ref, LineOrder: 0},
		{AccountID: revenueID, Credit: amount, Memo: "Revenue earned " + ref, LineOrder: 1},
	}
	eventID := "accrue:" + ref
	src := "iag.finance"
	return r.BookPostedEntry(ctx, CreateJournalParams{
		Description:    "Accrue revenue " + ref,
		SourceEventID:  &eventID,
		SourceService:  &src,
		AccountingDate: postedAt,
		Lines:          lines,
	}, eventID, "finance.revenue.accrue", postedAt, nil, audit)
}

// GetScheduleBySourceRef returns the schedule for a source ref.
func (r *Repository) GetScheduleBySourceRef(ctx context.Context, sourceRef string) (*RevenueSchedule, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, source_ref, total::text, currency, method, start_period, periods, status, created_at
		FROM revenue_schedules WHERE source_ref = $1
	`, sourceRef)
	return scanSchedule(row)
}

// ListSchedules returns recent schedules, newest first.
func (r *Repository) ListSchedules(ctx context.Context, limit int) ([]RevenueSchedule, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_ref, total::text, currency, method, start_period, periods, status, created_at
		FROM revenue_schedules ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RevenueSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func scanSchedule(row scannable) (*RevenueSchedule, error) {
	var s RevenueSchedule
	var total string
	if err := row.Scan(&s.ID, &s.SourceRef, &total, &s.Currency, &s.Method, &s.StartPeriod, &s.Periods, &s.Status, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScheduleNotFound
		}
		return nil, err
	}
	s.Total, _ = decimal.NewFromString(total)
	return &s, nil
}

// ratableSlices splits total into n slices rounded to 2dp, the last absorbing
// any rounding remainder so the slices sum exactly to total.
func ratableSlices(total decimal.Decimal, n int) []decimal.Decimal {
	out := make([]decimal.Decimal, n)
	per := total.DivRound(decimal.NewFromInt(int64(n)), 2)
	acc := decimal.Zero
	for i := 0; i < n-1; i++ {
		out[i] = per
		acc = acc.Add(per)
	}
	out[n-1] = total.Sub(acc)
	return out
}

// addMonths returns the 'YYYY-MM' period n months after base.
func addMonths(base string, n int) string {
	t, err := time.Parse("2006-01", base)
	if err != nil {
		return base
	}
	return t.AddDate(0, n, 0).Format("2006-01")
}
