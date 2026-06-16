package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/repository"
)

// ERP event types published by iag-erp on iag.operations (keep in sync with iag-erp/internal/events/types.go).
const (
	erpEmployeeCreated    = "erp.employee.created"
	erpEmployeeUpdated    = "erp.employee.updated"
	erpEmployeeTerminated = "erp.employee.terminated"
	erpLeaveApproved      = "erp.leave.approved"
	erpLeaveRejected      = "erp.leave.rejected"
	erpLeaveCancelled     = "erp.leave.cancelled"
)

type erpHandler struct {
	repo *repository.Repository
}

func (h *erpHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case erpEmployeeCreated, erpEmployeeUpdated, erpEmployeeTerminated:
		return h.handleEmployee(ctx, env)
	case erpLeaveApproved, erpLeaveRejected, erpLeaveCancelled:
		return h.handleLeave(ctx, env)
	default:
		return nil
	}
}

type erpEmployeeData struct {
	EmployeeNo     string `json:"employee_no"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Status         string `json:"status"`
	EmploymentType string `json:"employment_type"`
	JobTitle       string `json:"job_title"`
	DepartmentCode string `json:"department_code"`
	OperatorRef    string `json:"operator_ref"`
	UserID         string `json:"user_id"`
}

type erpLeaveData struct {
	LeaveRequestID string  `json:"leave_request_id"`
	EmployeeNo     string  `json:"employee_no"`
	LeaveTypeCode  string  `json:"leave_type_code"`
	StartsOn       string  `json:"starts_on"`
	EndsOn         string  `json:"ends_on"`
	Days           float64 `json:"days"`
	Status         string  `json:"status"`
}

func (h *erpHandler) handleEmployee(ctx context.Context, env platformevents.Envelope) error {
	var data erpEmployeeData
	if err := remarshal(env.Data, &data); err != nil {
		return platformevents.Permanent(err)
	}
	data.EmployeeNo = strings.TrimSpace(data.EmployeeNo)
	if data.EmployeeNo == "" {
		return platformevents.Permanent(errMissingERPEmployeeNo)
	}
	status := strings.TrimSpace(data.Status)
	if env.Type == erpEmployeeTerminated {
		status = "terminated"
	}
	if status == "" {
		status = "active"
	}
	var userID *uuid.UUID
	if raw := strings.TrimSpace(data.UserID); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return platformevents.Permanent(err)
		}
		userID = &id
	}
	err := h.repo.UpsertPayrollEmployeeRef(ctx, repository.PayrollEmployeeUpsert{
		EmployeeNo:     data.EmployeeNo,
		UserID:         userID,
		FirstName:      strings.TrimSpace(data.FirstName),
		LastName:       strings.TrimSpace(data.LastName),
		DepartmentCode: strings.TrimSpace(data.DepartmentCode),
		JobTitle:       strings.TrimSpace(data.JobTitle),
		EmploymentType: strings.TrimSpace(data.EmploymentType),
		Status:         status,
		OperatorRef:    strings.TrimSpace(data.OperatorRef),
		EventID:        env.ID,
		EventType:      env.Type,
		EventTime:      parseEnvTime(env.Time),
	})
	if err != nil {
		return err
	}
	slog.Info("finance payroll employee mirror updated", "employee_no", data.EmployeeNo, "event", env.Type)
	return nil
}

func (h *erpHandler) handleLeave(ctx context.Context, env platformevents.Envelope) error {
	var data erpLeaveData
	if err := remarshal(env.Data, &data); err != nil {
		return platformevents.Permanent(err)
	}
	data.LeaveRequestID = strings.TrimSpace(data.LeaveRequestID)
	data.EmployeeNo = strings.TrimSpace(data.EmployeeNo)
	if data.LeaveRequestID == "" || data.EmployeeNo == "" {
		return platformevents.Permanent(errMissingERPLeaveFields)
	}
	start, err1 := time.Parse("2006-01-02", strings.TrimSpace(data.StartsOn))
	end, err2 := time.Parse("2006-01-02", strings.TrimSpace(data.EndsOn))
	if err1 != nil || err2 != nil {
		return platformevents.Permanent(errors.New("erp leave event missing valid starts_on/ends_on"))
	}
	accrualStatus := strings.TrimSpace(data.Status)
	if accrualStatus == "" {
		switch env.Type {
		case erpLeaveApproved:
			accrualStatus = "approved"
		case erpLeaveRejected:
			accrualStatus = "rejected"
		case erpLeaveCancelled:
			accrualStatus = "cancelled"
		}
	}
	if err := h.repo.RecordPayrollLeaveAccrual(ctx, repository.PayrollLeaveAccrualInput{
		LeaveRequestID: data.LeaveRequestID,
		EmployeeNo:     data.EmployeeNo,
		LeaveTypeCode:  strings.TrimSpace(data.LeaveTypeCode),
		StartsOn:       start,
		EndsOn:         end,
		Days:           fmt.Sprintf("%.2f", data.Days),
		AccrualStatus:  accrualStatus,
		EventID:        env.ID,
		EventType:      env.Type,
	}); err != nil {
		return err
	}
	slog.Info("finance payroll leave accrual recorded", "employee_no", data.EmployeeNo, "leave_request_id", data.LeaveRequestID, "event", env.Type)
	return nil
}

var (
	errMissingERPEmployeeNo  = errors.New("erp employee event missing employee_no")
	errMissingERPLeaveFields = errors.New("erp leave event missing leave_request_id or employee_no")
)

// NewERP builds a consumer for iag-erp HR events on iag.operations.
func NewERP(cfg Config, repo *repository.Repository, dlq *platformevents.Producer) (*Consumer, error) {
	h := &erpHandler{repo: repo}
	inner, err := platformevents.NewConsumer(platformevents.ConsumerConfig{
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
		Handler:     h,
		Dedupe:      platformevents.NoopDedupe{},
		DLQProducer: dlq,
		DLQTopic:    cfg.DLQTopic,
	})
	if err != nil {
		return nil, err
	}
	return &Consumer{inner: inner}, nil
}

// ERPHandledTypes lists event types consumed for payroll mirror (tests/docs).
func ERPHandledTypes() []string {
	return []string{
		erpEmployeeCreated, erpEmployeeUpdated, erpEmployeeTerminated,
		erpLeaveApproved, erpLeaveRejected, erpLeaveCancelled,
	}
}

// parseEnvTime parses the envelope's RFC3339 time, falling back to the zero
// time (treated as "epoch" by the mirror's ordering guard) when absent/invalid.
func parseEnvTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
