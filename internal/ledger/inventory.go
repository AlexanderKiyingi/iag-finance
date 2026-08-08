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
	wipAccount       = "1450" // Work in Progress (asset)
	grirAccount      = "2150" // GR/IR Clearing (liability)
	cogsAccount      = "5000" // Cost of Goods Sold (expense)
)

// CostNeutralMovements are the movement types that legitimately have no GL
// effect: stock changing place or custody without changing hands or value.
//
// It is exported so a cross-service test can assert that every movement type
// warehouse emits is either mapped below or named here. Production spent a
// release booking nothing because it was in neither list and fell into the
// default — the two services drifted apart quietly because nothing compared
// them.
var CostNeutralMovements = map[string]string{
	"transfer":       "stock moves between bins; value does not change",
	"pick":           "staged for dispatch, not yet consumed",
	"asset_checkin":  "custody returns; the asset is already capitalised",
	"asset_checkout": "custody moves; the asset is already capitalised",
	"asset_dispose":  "booked from warehouse.asset.disposed, not the movement",
}

// BookInventoryMovement books the GL effect of a valued stock movement:
//
//	receipt    → Dr 1400 Inventory / Cr 2150 GR/IR      (goods in, awaiting bill)
//	issue      → Dr 5000 COGS      / Cr 1400 Inventory  (goods out at avg cost)
//	adjustment → increase: Dr 1400 / Cr 5000; decrease: Dr 5000 / Cr 1400
//	production_consume → Dr 1450 WIP       / Cr 1400 Inventory (material into a run)
//	production_output  → Dr 1400 Inventory / Cr 1450 WIP       (finished goods out)
//	cost-neutral types / unknown / zero cost → no GL (returns nil, nil)
//
// Production is costed material-only: output is valued at what its order
// consumed, so a completed order clears WIP to zero and any residue is yield
// loss. Labour and energy stay as period expense.
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

	lines := linesForMovement(movementType, amt, totalCost, memo)
	if len(lines) == 0 {
		// Cost-neutral by design, or a type this service does not know about.
		// Either way there is no honest posting to make;
		// TestEveryWarehouseMovementIsAccounted is what stops an unknown type
		// staying unnoticed.
		return nil, nil
	}

	desc := "Inventory movement " + movementType
	if ref != "" {
		desc += " " + ref
	}
	return s.BookFromEvent(ctx, eventID, eventType, source, correlationID, desc, currency, lines)
}

// linesForMovement is the movement-type → journal-lines mapping, pure so the
// posting shape is testable without a ledger or a database. An empty result
// means cost-neutral.
//
// amt is the absolute value; totalCost keeps its sign, which is what tells an
// adjustment whether stock went up or down.
func linesForMovement(movementType string, amt, totalCost decimal.Decimal, memo string) []LineInput {
	switch movementType {
	case "receipt":
		return []LineInput{
			{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
			{AccountCode: grirAccount, Credit: amt, Memo: memo},
		}
	case "issue":
		return []LineInput{
			{AccountCode: cogsAccount, Debit: amt, Memo: memo},
			{AccountCode: inventoryAccount, Credit: amt, Memo: memo},
		}
	case "return":
		// Stock comes back, so the cost of sale reverses. Warehouse defines this
		// movement but does not yet emit it; mapping it now means the return
		// path books correctly the day it is built, rather than falling into
		// the cost-neutral default and counting goods as consumed and held at
		// the same time.
		return []LineInput{
			{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
			{AccountCode: cogsAccount, Credit: amt, Memo: memo},
		}
	case "production_consume":
		// Material leaves stock for a run. It is not an expense yet — it is
		// still an asset, just one being worked on — so it parks in WIP rather
		// than going straight to COGS.
		return []LineInput{
			{AccountCode: wipAccount, Debit: amt, Memo: memo},
			{AccountCode: inventoryAccount, Credit: amt, Memo: memo},
		}
	case "production_output":
		// Finished goods arrive, valued at what the order consumed, which is
		// what clears that order's WIP back to zero.
		return []LineInput{
			{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
			{AccountCode: wipAccount, Credit: amt, Memo: memo},
		}
	case "adjustment":
		if totalCost.IsNegative() { // write-down: reduce inventory, expense the loss
			return []LineInput{
				{AccountCode: cogsAccount, Debit: amt, Memo: memo},
				{AccountCode: inventoryAccount, Credit: amt, Memo: memo},
			}
		}
		return []LineInput{ // write-up: increase inventory, reverse cost
			{AccountCode: inventoryAccount, Debit: amt, Memo: memo},
			{AccountCode: cogsAccount, Credit: amt, Memo: memo},
		}
	default:
		return nil
	}
}
