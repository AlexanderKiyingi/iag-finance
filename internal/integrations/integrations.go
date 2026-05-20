package integrations

import "time"

type Status string

const (
	StatusStub    Status = "stub"
	StatusReady   Status = "ready"
	StatusError   Status = "error"
	StatusOffline Status = "offline"
)

type Health struct {
	Name        string    `json:"name"`
	Status      Status    `json:"status"`
	Description string    `json:"description"`
	CheckedAt   time.Time `json:"checkedAt"`
}

func FinanceHealth() []Health {
	now := time.Now().UTC()
	return []Health{
		{
			Name:        "ura-efris",
			Status:      StatusStub,
			Description: "URA EFRIS e-invoicing — adapter not configured (Phase 1 stub)",
			CheckedAt:   now,
		},
		{
			Name:        "banking",
			Status:      StatusStub,
			Description: "Bank reconciliation — adapter not configured (Phase 1 stub)",
			CheckedAt:   now,
		},
	}
}

func URAStatus() Health {
	h := FinanceHealth()
	return h[0]
}

func BankingStatus() Health {
	h := FinanceHealth()
	return h[1]
}
