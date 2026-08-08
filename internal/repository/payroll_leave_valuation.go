package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// LeaveBalanceUpsert is a balance reported by HR.
type LeaveBalanceUpsert struct {
	EmployeeNo    string
	LeaveTypeCode string
	AccrualYear   int
	BalanceDays   string
	EventID       string
}

// UpsertLeaveBalance records the days HR says are owed.
func (r *Repository) UpsertLeaveBalance(ctx context.Context, in LeaveBalanceUpsert) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payroll_leave_balances
			(employee_no, leave_type_code, accrual_year, balance_days, source_event_id, updated_at)
		VALUES ($1, $2, $3, $4::numeric, $5, NOW())
		ON CONFLICT (employee_no, leave_type_code, accrual_year) DO UPDATE
		SET balance_days = EXCLUDED.balance_days,
		    source_event_id = EXCLUDED.source_event_id,
		    updated_at = NOW()`,
		in.EmployeeNo, in.LeaveTypeCode, in.AccrualYear, in.BalanceDays, in.EventID)
	return err
}

// EmployeeRateUpsert is a daily rate reported by HR.
type EmployeeRateUpsert struct {
	EmployeeNo    string
	DailyRate     string
	Currency      string
	EffectiveFrom time.Time
}

// UpsertEmployeeRate records what a day of an employee's time is worth.
func (r *Repository) UpsertEmployeeRate(ctx context.Context, in EmployeeRateUpsert) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payroll_employee_rates (employee_no, daily_rate, currency, effective_from, updated_at)
		VALUES ($1, $2::numeric, $3, $4, NOW())
		ON CONFLICT (employee_no) DO UPDATE
		SET daily_rate = EXCLUDED.daily_rate,
		    currency = EXCLUDED.currency,
		    effective_from = EXCLUDED.effective_from,
		    updated_at = NOW()`,
		in.EmployeeNo, in.DailyRate, in.Currency, in.EffectiveFrom)
	return err
}

// LeaveLiability is the measured obligation at a point in time.
type LeaveLiability struct {
	Total            decimal.Decimal
	EmployeesValued  int
	EmployeesUnrated int
}

// MeasureLeaveLiability values every reported balance at its employee's rate.
//
// Employees with a balance but no rate are counted, not skipped quietly: their
// obligation is real and unmeasured, and a total that omits them without saying
// so reads as complete.
func (r *Repository) MeasureLeaveLiability(ctx context.Context, year int) (*LeaveLiability, error) {
	var (
		out   LeaveLiability
		total string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(b.balance_days * rt.daily_rate) FILTER (WHERE rt.daily_rate IS NOT NULL), 0)::text,
			COUNT(*) FILTER (WHERE rt.daily_rate IS NOT NULL),
			COUNT(*) FILTER (WHERE rt.daily_rate IS NULL AND b.balance_days > 0)
		FROM payroll_leave_balances b
		LEFT JOIN payroll_employee_rates rt ON rt.employee_no = b.employee_no
		WHERE b.accrual_year = $1 AND b.balance_days > 0`, year).
		Scan(&total, &out.EmployeesValued, &out.EmployeesUnrated)
	if err != nil {
		return nil, err
	}
	out.Total, err = decimal.NewFromString(total)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LastLeaveValuationTotal returns the most recently booked liability, or zero.
// The next valuation books the difference from this, not the whole figure.
func (r *Repository) LastLeaveValuationTotal(ctx context.Context) (decimal.Decimal, error) {
	var total string
	err := r.pool.QueryRow(ctx, `
		SELECT total_liability::text FROM payroll_leave_valuations
		ORDER BY valued_at DESC LIMIT 1`).Scan(&total)
	if errors.Is(err, pgx.ErrNoRows) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(total)
}

// LeaveValuationParams records a booked valuation.
type LeaveValuationParams struct {
	ValuedAt         time.Time
	TotalLiability   string
	Movement         string
	Currency         string
	EmployeesValued  int
	EmployeesUnrated int
	JournalEntryID   *uuid.UUID
}

// RecordLeaveValuation stores a valuation. One per date: re-running a day
// replaces it rather than stacking movements on the same closing figure.
func (r *Repository) RecordLeaveValuation(ctx context.Context, in LeaveValuationParams) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payroll_leave_valuations
			(valued_at, total_liability, movement, currency, employees_valued, employees_unrated, journal_entry_id)
		VALUES ($1, $2::numeric, $3::numeric, $4, $5, $6, $7)
		ON CONFLICT (valued_at) DO UPDATE
		SET total_liability = EXCLUDED.total_liability,
		    movement = EXCLUDED.movement,
		    employees_valued = EXCLUDED.employees_valued,
		    employees_unrated = EXCLUDED.employees_unrated,
		    journal_entry_id = EXCLUDED.journal_entry_id`,
		in.ValuedAt, in.TotalLiability, in.Movement, in.Currency,
		in.EmployeesValued, in.EmployeesUnrated, in.JournalEntryID)
	return err
}
