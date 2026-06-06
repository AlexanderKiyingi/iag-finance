package integrations

import (
	"github.com/iag-finance/backend/internal/config"
)

// Registry holds configured outbound integration adapters.
type Registry struct {
	EFRIS EFRISAdapter
	Bank  BankFeedAdapter
}

// NewRegistry builds adapters from environment (HTTP when URLs set, stub otherwise).
func NewRegistry(cfg config.Config) *Registry {
	return &Registry{
		EFRIS: newEFRISAdapter(cfg),
		Bank:  newBankFeedAdapter(cfg),
	}
}

func (r *Registry) EFRISMode() string {
	if r == nil || r.EFRIS == nil {
		return "stub"
	}
	return r.EFRIS.Mode()
}

func (r *Registry) BankMode() string {
	if r == nil || r.Bank == nil {
		return "stub"
	}
	return r.Bank.Mode()
}
