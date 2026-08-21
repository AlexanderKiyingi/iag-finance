package consumer

import (
	"os"
	"strings"
	"testing"
)

// docs/EVENT_CONTRACT.md is what other teams integrate against. Nothing tied it
// to the code, and it drifted: payments.settled, party.vendor.upserted,
// warehouse.movement.posted, erp.payroll.run_posted, erp.leave.balance_changed
// and erp.employee.rate_changed were all being consumed and acted on with no
// mention in the contract. Two whole consumer groups were undocumented.
const eventContractPath = "../../docs/EVENT_CONTRACT.md"

func TestEventContractDocumentsEveryHandledType(t *testing.T) {
	contract := readContract(t)

	for _, et := range HandledTypes() {
		if !strings.Contains(contract, et) {
			t.Errorf("finance consumes %q but EVENT_CONTRACT.md never mentions it — "+
				"an integrator reading the contract would not know finance already acts on it", et)
		}
	}
}

func TestEventContractDocumentsEveryConsumerGroup(t *testing.T) {
	contract := readContract(t)

	for _, s := range Subscriptions() {
		if !strings.Contains(contract, s.Group) {
			t.Errorf("consumer group %q is not in EVENT_CONTRACT.md — each group replays "+
				"independently, so an operator needs it listed to reset one", s.Group)
		}
	}
}

func TestNoEventTypeIsDeclaredTwice(t *testing.T) {
	seen := map[string]string{}
	for _, s := range Subscriptions() {
		for _, et := range s.Types {
			if prev, dup := seen[et]; dup {
				t.Errorf("%q is declared under both %q and %q; two groups on one type "+
					"means it books twice", et, prev, s.Group)
			}
			seen[et] = s.Group
		}
	}
}

func readContract(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(eventContractPath)
	if err != nil {
		t.Fatalf("read %s: %v", eventContractPath, err)
	}
	return string(b)
}
