package consumer

import (
	"context"
	"log/slog"
	"strings"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/ledger"
)

const warehouseAssetDisposed = "warehouse.asset.disposed"

// warehouseHandler books finance effects of stores (warehouse) events on
// iag.operations. Today: asset disposal → gain/loss on disposal.
type warehouseHandler struct {
	ledger *ledger.Service
}

func (h *warehouseHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case warehouseAssetDisposed:
		return h.handleAssetDisposed(ctx, env)
	default:
		return nil
	}
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
