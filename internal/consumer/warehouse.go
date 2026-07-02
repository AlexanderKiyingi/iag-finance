package consumer

import (
	"context"
	"log/slog"
	"strings"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
)

const (
	warehouseAssetDisposed  = "warehouse.asset.disposed"
	warehouseMovementPosted = "warehouse.movement.posted"
)

// warehouseHandler books finance effects of stores (warehouse) events on
// iag.operations: asset disposal → gain/loss; valued stock movements →
// perpetual-inventory GL (dormant until warehouse emits cost — see
// handleMovementPosted).
type warehouseHandler struct {
	ledger *ledger.Service
}

func (h *warehouseHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case warehouseAssetDisposed:
		return h.handleAssetDisposed(ctx, env)
	case warehouseMovementPosted:
		return h.handleMovementPosted(ctx, env)
	default:
		return nil
	}
}

// handleMovementPosted books the GL for a valued warehouse stock movement
// (receipt → Dr 1400/Cr 2150, issue → Dr 5000/Cr 1400, adjustment → net 1400).
// It is deliberately DORMANT until the warehouse emitter includes cost: a
// movement with no/zero total_cost books nothing (BookInventoryMovement returns
// a clean no-op), so enabling the finance consumer ahead of the emitter is safe.
// Idempotent on env.ID (the warehouse movement_id).
func (h *warehouseHandler) handleMovementPosted(ctx context.Context, env platformevents.Envelope) error {
	data := env.Data
	if data == nil {
		return nil
	}
	movementType, _ := data["movement_type"].(string)
	ref, _ := data["ref"].(string)
	currency, _ := data["currency"].(string)
	if strings.TrimSpace(currency) == "" {
		currency = "UGX"
	}
	totalCost := disposalAmount(data["total_cost"]) // shared numeric/string coercion

	entry, err := h.ledger.BookInventoryMovement(ctx, env.ID, env.Type, env.Source, env.CorrelationID,
		strings.TrimSpace(movementType), strings.TrimSpace(ref), currency, totalCost)
	if err != nil {
		return err
	}
	if entry != nil {
		slog.Info("finance booked inventory movement", "movementType", movementType, "ref", ref, "entry", entry.EntryNumber)
	}
	return nil
}

func (h *warehouseHandler) handleAssetDisposed(ctx context.Context, env platformevents.Envelope) error {
	data := env.Data
	if data == nil {
		return nil
	}
	assetTag, _ := data["asset_tag"].(string)
	method, _ := data["method"].(string)
	currency, _ := data["currency"].(string)
	if strings.TrimSpace(currency) == "" {
		currency = "UGX"
	}
	proceeds := disposalAmount(data["proceeds"])
	bookValue := disposalAmount(data["book_value"])

	entry, err := h.ledger.BookAssetDisposal(ctx, env.ID, env.Type, env.Source, env.CorrelationID, currency, assetTag, method, proceeds, bookValue)
	if err != nil {
		return err
	}
	if entry != nil {
		slog.Info("finance booked asset disposal", "assetTag", assetTag, "method", method, "entry", entry.EntryNumber)
	}
	return nil
}

// disposalAmount coerces an event amount (emitted as a JSON number, occasionally
// a string) into a decimal; anything unparseable is zero.
func disposalAmount(v any) decimal.Decimal {
	switch x := v.(type) {
	case float64:
		return decimal.NewFromFloat(x)
	case string:
		d, err := decimal.NewFromString(x)
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
	}
}

// NewWarehouse builds a consumer for warehouse.* events on iag.operations.
func NewWarehouse(cfg Config, ledgerSvc *ledger.Service, dlq *platformevents.Producer) (*Consumer, error) {
	h := &warehouseHandler{ledger: ledgerSvc}
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
