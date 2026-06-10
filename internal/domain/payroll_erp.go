package domain

import (
	"time"

	"github.com/google/uuid"
)

// PayrollEmployeeRef mirrors iag-erp workforce master for payroll journal prep.
type PayrollEmployeeRef struct {
	EmployeeNo     string     `json:"employee_no"`
	UserID         *uuid.UUID `json:"user_id,omitempty"`
	FirstName      string     `json:"first_name"`
	LastName       string     `json:"last_name"`
	DepartmentCode *string    `json:"department_code,omitempty"`
	JobTitle       string     `json:"job_title"`
	EmploymentType string     `json:"employment_type"`
	Status         string     `json:"status"`
	OperatorRef    *string    `json:"operator_ref,omitempty"`
	LastEventID    string     `json:"last_event_id"`
	LastEventType  string     `json:"last_event_type"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// PayrollLeaveAccrual records leave workflow events for payroll accrual/reversal.
type PayrollLeaveAccrual struct {
	ID              uuid.UUID `json:"id"`
	LeaveRequestID  string    `json:"leave_request_id"`
	EmployeeNo      string    `json:"employee_no"`
	LeaveTypeCode   string    `json:"leave_type_code"`
	StartsOn        time.Time `json:"starts_on"`
	EndsOn          time.Time `json:"ends_on"`
	Days            string    `json:"days"`
	AccrualStatus   string    `json:"accrual_status"`
	SourceEventID   string    `json:"source_event_id"`
	SourceEventType string    `json:"source_event_type"`
	CreatedAt       time.Time `json:"created_at"`
}
