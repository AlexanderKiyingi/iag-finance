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

// LeaveBalanceRow is a reported balance with the rate it would be valued at.
type LeaveBalanceRow struct {
	EmployeeNo    string  `json:"employeeNo"`
	LeaveTypeCode string  `json:"leaveTypeCode"`
	AccrualYear   int     `json:"accrualYear"`
	BalanceDays   string  `json:"balanceDays"`
	DailyRate     *string `json:"dailyRate,omitempty"`
	Value         *string `json:"value,omitempty"`
	Currency      string  `json:"currency,omitempty"`
}

// ListLeaveBalances returns reported balances joined to their rate.
//
// A balance with no rate comes back with a null rate and value rather than
// being hidden: it is an obligation nobody can measure, and the list is where
// that should be visible.
func (r *Repository) ListLeaveBalances(ctx context.Context, year int, limit int) ([]LeaveBalanceRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT b.employee_no, b.leave_type_code, b.accrual_year, b.balance_days::text,
		       rt.daily_rate::text,
		       CASE WHEN rt.daily_rate IS NULL THEN NULL
		            ELSE (b.balance_days * rt.daily_rate)::text END,
		       COALESCE(rt.currency, '')
		FROM payroll_leave_balances b
		LEFT JOIN payroll_employee_rates rt ON rt.employee_no = b.employee_no
		WHERE ($1 = 0 OR b.accrual_year = $1)
		ORDER BY b.employee_no, b.leave_type_code
		LIMIT $2`, year, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeaveBalanceRow{}
	for rows.Next() {
		var b LeaveBalanceRow
		if err := rows.Scan(&b.EmployeeNo, &b.LeaveTypeCode, &b.AccrualYear,
			&b.BalanceDays, &b.DailyRate, &b.Value, &b.Currency); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// LeaveValuationRow is a booked valuation.
type LeaveValuationRow struct {
	ValuedAt         time.Time  `json:"valuedAt"`
	TotalLiability   string     `json:"totalLiability"`
	Movement         string     `json:"movement"`
	Currency         string     `json:"currency"`
	EmployeesValued  int        `json:"employeesValued"`
	EmployeesUnrated int        `json:"employeesUnrated"`
	JournalEntryID   *uuid.UUID `json:"journalEntryId,omitempty"`
}

// ListLeaveValuations returns booked valuations, newest first.
func (r *Repository) ListLeaveValuations(ctx context.Context, limit int) ([]LeaveValuationRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT valued_at, total_liability::text, movement::text, currency,
		       employees_valued, employees_unrated, journal_entry_id
		FROM payroll_leave_valuations
		ORDER BY valued_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeaveValuationRow{}
	for rows.Next() {
		var v LeaveValuationRow
		if err := rows.Scan(&v.ValuedAt, &v.TotalLiability, &v.Movement, &v.Currency,
			&v.EmployeesValued, &v.EmployeesUnrated, &v.JournalEntryID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
