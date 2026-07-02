package ledger

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
)

// Perpetual-inventory control accounts (seeded: migrations 032, 040, and the
// GR/IR clearing account). Valuation lives in iag-warehouse; finance only books
// the GL from valued warehouse.movement.posted events. See
// docs/GAP_REMEDIATION_ROADMAP.md and warehouse docs/PERPETUAL_INVENTORY_EVENTS.md.
const (
	inventoryAccount = "1400" // Inventory (asset)
	grirAccount      = "2150" // GR/IR Clearing (liability)
	cogsAccount      = "5000" // Cost of Goods Sold (expense)
)

// BookInventoryMovement books the GL effect of a valued stock movement:
//
//	receipt    → Dr 1400 Inventory / Cr 2150 GR/IR      (goods in, awaiting bill)
//	issue      → Dr 5000 COGS      / Cr 1400 Inventory  (goods out at avg cost)
//	adjustment → increase: Dr 1400 / Cr 5000; decrease: Dr 5000 / Cr 1400
//	transfer / unknown / zero cost → no GL (returns nil, nil)
//
// Idempotent on eventID (the warehouse movement_id) via BookFromEvent. Returns
// (nil, nil) — a clean no-op — when totalCost is zero or absent, which is what
// keeps the consumer dormant until warehouse emits valued movements.
func (s *Service) BookInventoryMovement(ctx context.Context, eventID, eventType, source, correlationID, movementType, ref, currency string, totalCost decimal.Decimal) (*domain.JournalEntry, error) {
	if currency == "" {
		currency = s.repo.BaseCurrency()
	}
	amt := totalCost.Abs()
	if amt.IsZero() {
		return nil, nil // transfer / cost-less movement, or costing disabled upstream
	}

	memo := "Inventory " + movementType
	if ref != "" {
		memo += " " + ref
	}

	var lines []LineInput
	switch movementType {
	case "receipt":
		lines = []LineInput{
			{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
			{AccountCode: grirAccount, Credit: amt, Memo: memo},
		}
	case "issue":
		lines = []LineInput{
			{AccountCode: cogsAccount, Debit: amt, Memo: memo},
			{AccountCode: inventoryAccount, Credit: amt, Memo: memo},
		}
	case "adjustment":
		if totalCost.IsNegative() { // write-down: reduce inventory, expense the loss
			lines = []LineInput{
				{AccountCode: cogsAccount, Debit: amt, Memo: memo},
				{AccountCode: inventoryAccount, Credit: amt, Memo: memo},
			}
		} else { // write-up: increase inventory, reverse cost
			lines = []LineInput{
				{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
				{AccountCode: cogsAccount, Credit: amt, Memo: memo},
			}
		}
	default:
		return nil, nil // transfer between bins, or an unknown type → cost-neutral
	}

	desc := "Inventory movement " + movementType
	if ref != "" {
		desc += " " + ref
	}
	return s.BookFromEvent(ctx, eventID, eventType, source, correlationID, desc, currency, lines)
}
