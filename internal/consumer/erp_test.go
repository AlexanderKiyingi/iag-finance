package consumer

import (
	"context"
	"testing"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"

	"github.com/iag-finance/backend/internal/ledger"
)

// A run with no ref or period can never be posted, so it must be rejected
// permanently rather than redelivered until the DLQ picks it up on age.
func TestERPHandler_payrollMissingFields(t *testing.T) {
	h := &erpHandler{ledger: &ledger.Service{}}
	err := h.handlePayrollRun(context.Background(), platformevents.Envelope{
		Type: erpPayrollRunPosted,
		ID:   "evt-3",
		Data: map[string]any{"gross": 1000000},
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
}

// Deployments that run the mirror without a ledger must ignore payroll rather
// than panic on the nil service.
func TestERPHandler_payrollWithoutLedgerIsIgnored(t *testing.T) {
	h := &erpHandler{}
	err := h.Handle(context.Background(), platformevents.Envelope{
		Type: erpPayrollRunPosted,
		ID:   "evt-4",
		Data: map[string]any{"run_ref": "PR-2026-07-ABC123", "period": "2026-07"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestERPHandler_ignoresUnknown(t *testing.T) {
	h := &erpHandler{}
	if err := h.Handle(context.Background(), platformevents.Envelope{Type: "erp.production_order.updated"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestERPHandler_employeeMissingNo(t *testing.T) {
	h := &erpHandler{repo: nil}
	err := h.handleEmployee(context.Background(), platformevents.Envelope{
		Type: erpEmployeeCreated,
		ID:   "evt-1",
		Data: map[string]any{"first_name": "Jane"},
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
}

func TestERPHandler_leaveMissingFields(t *testing.T) {
	h := &erpHandler{repo: nil}
	err := h.handleLeave(context.Background(), platformevents.Envelope{
		Type: erpLeaveApproved,
		ID:   "evt-2",
		Data: map[string]any{"employee_no": "EMP-001"},
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
}
