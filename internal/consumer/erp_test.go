package consumer

import (
	"testing"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
)

func TestERPHandledTypes(t *testing.T) {
	types := ERPHandledTypes()
	if len(types) != 6 {
		t.Fatalf("len = %d, want 6", len(types))
	}
	seen := map[string]bool{}
	for _, et := range types {
		if seen[et] {
			t.Fatalf("duplicate %q", et)
		}
		seen[et] = true
	}
	if !seen["erp.employee.created"] || !seen["erp.leave.approved"] {
		t.Fatalf("missing expected types: %#v", types)
	}
}

func TestERPHandler_ignoresUnknown(t *testing.T) {
	h := &erpHandler{}
	if err := h.Handle(t.Context(), platformevents.Envelope{Type: "erp.production_order.updated"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestERPHandler_employeeMissingNo(t *testing.T) {
	h := &erpHandler{repo: nil}
	err := h.handleEmployee(t.Context(), platformevents.Envelope{
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
	err := h.handleLeave(t.Context(), platformevents.Envelope{
		Type: erpLeaveApproved,
		ID:   "evt-2",
		Data: map[string]any{"employee_no": "EMP-001"},
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
}
