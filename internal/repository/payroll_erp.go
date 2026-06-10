package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iag-finance/backend/internal/domain"
)

type PayrollEmployeeUpsert struct {
	EmployeeNo     string
	UserID         *uuid.UUID
	FirstName      string
	LastName       string
	DepartmentCode string
	JobTitle       string
	EmploymentType string
	Status         string
	OperatorRef    string
	EventID        string
	EventType      string
}

type PayrollLeaveAccrualInput struct {
	LeaveRequestID string
	EmployeeNo     string
	LeaveTypeCode  string
	StartsOn       time.Time
	EndsOn         time.Time
	Days           string
	AccrualStatus  string
	EventID        string
	EventType      string
}

func (r *Repository) UpsertPayrollEmployeeRef(ctx context.Context, in PayrollEmployeeUpsert) error {
	employeeNo := strings.TrimSpace(in.EmployeeNo)
	if employeeNo == "" || strings.TrimSpace(in.EventID) == "" {
		return errPayrollBadInput
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payroll_employee_refs (
		  employee_no, user_id, first_name, last_name, department_code, job_title,
		  employment_type, status, operator_ref, last_event_id, last_event_type, updated_at
		) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,NULLIF($9,''),$10,$11,NOW())
		ON CONFLICT (employee_no) DO UPDATE SET
		  user_id = COALESCE(EXCLUDED.user_id, payroll_employee_refs.user_id),
		  first_name = EXCLUDED.first_name,
		  last_name = EXCLUDED.last_name,
		  department_code = COALESCE(EXCLUDED.department_code, payroll_employee_refs.department_code),
		  job_title = EXCLUDED.job_title,
		  employment_type = EXCLUDED.employment_type,
		  status = EXCLUDED.status,
		  operator_ref = COALESCE(EXCLUDED.operator_ref, payroll_employee_refs.operator_ref),
		  last_event_id = EXCLUDED.last_event_id,
		  last_event_type = EXCLUDED.last_event_type,
		  updated_at = NOW()`,
		employeeNo, in.UserID, in.FirstName, in.LastName, in.DepartmentCode, in.JobTitle,
		in.EmploymentType, in.Status, in.OperatorRef, in.EventID, in.EventType)
	return err
}

func (r *Repository) RecordPayrollLeaveAccrual(ctx context.Context, in PayrollLeaveAccrualInput) error {
	if strings.TrimSpace(in.LeaveRequestID) == "" || strings.TrimSpace(in.EmployeeNo) == "" || strings.TrimSpace(in.EventID) == "" {
		return errPayrollBadInput
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payroll_leave_accruals (
		  leave_request_id, employee_no, leave_type_code, starts_on, ends_on, days,
		  accrual_status, source_event_id, source_event_type
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (source_event_id) DO NOTHING`,
		in.LeaveRequestID, in.EmployeeNo, in.LeaveTypeCode, in.StartsOn, in.EndsOn, in.Days,
		in.AccrualStatus, in.EventID, in.EventType)
	return err
}

func (r *Repository) ListPayrollEmployeeRefs(ctx context.Context, status string, limit int) ([]domain.PayrollEmployeeRef, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
		SELECT employee_no, user_id, first_name, last_name, department_code, job_title,
		       employment_type, status, operator_ref, last_event_id, last_event_type, updated_at
		FROM payroll_employee_refs WHERE 1=1`
	args := []any{}
	if status != "" {
		q += ` AND status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY updated_at DESC LIMIT ` + itoa(limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PayrollEmployeeRef
	for rows.Next() {
		var e domain.PayrollEmployeeRef
		if err := rows.Scan(&e.EmployeeNo, &e.UserID, &e.FirstName, &e.LastName, &e.DepartmentCode,
			&e.JobTitle, &e.EmploymentType, &e.Status, &e.OperatorRef, &e.LastEventID, &e.LastEventType, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) ListPayrollLeaveAccruals(ctx context.Context, employeeNo, accrualStatus string, limit int) ([]domain.PayrollLeaveAccrual, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
		SELECT id, leave_request_id, employee_no, leave_type_code, starts_on, ends_on, days,
		       accrual_status, source_event_id, source_event_type, created_at
		FROM payroll_leave_accruals WHERE 1=1`
	args := []any{}
	n := 1
	if employeeNo != "" {
		q += ` AND employee_no = $` + itoa(n)
		args = append(args, employeeNo)
		n++
	}
	if accrualStatus != "" {
		q += ` AND accrual_status = $` + itoa(n)
		args = append(args, accrualStatus)
		n++
	}
	_ = n
	q += ` ORDER BY created_at DESC LIMIT ` + itoa(limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PayrollLeaveAccrual
	for rows.Next() {
		var a domain.PayrollLeaveAccrual
		if err := rows.Scan(&a.ID, &a.LeaveRequestID, &a.EmployeeNo, &a.LeaveTypeCode, &a.StartsOn, &a.EndsOn,
			&a.Days, &a.AccrualStatus, &a.SourceEventID, &a.SourceEventType, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

var errPayrollBadInput = pgx.ErrNoRows // sentinel; consumer maps invalid payloads to Permanent
