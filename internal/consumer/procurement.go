package consumer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"

	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/ledger"
)

const procurementInvoiceReceived = "procurement.invoice.received"

type procurementHandler struct {
	ledger *ledger.Service
	bus    *events.Bus
}

func (h *procurementHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	if env.Type != procurementInvoiceReceived {
		return nil
	}
	data := env.Data
	if data == nil {
		return nil
	}
	vendorRef, _ := data["vendorRef"].(string)
	documentRef, _ := data["documentRef"].(string)
	amount, _ := data["amount"].(string)
	currency, _ := data["currency"].(string)
	vendorRef = strings.TrimSpace(vendorRef)
	documentRef = strings.TrimSpace(documentRef)
	amount = strings.TrimSpace(amount)
	if documentRef == "" || amount == "" {
		return platformevents.Permanent(errMissingProcurementFields) //nolint:wrapcheck
	}
	if currency == "" {
		currency = "UGX"
	}
	var due *time.Time
	if raw, ok := data["dueDate"].(string); ok && strings.TrimSpace(raw) != "" {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
		if err != nil {
			return platformevents.Permanent(err)
		}
		due = &t
	}
	desc, _ := data["description"].(string)
	item, err := h.ledger.CreateAPItem(ctx, vendorRef, documentRef, desc, amount, currency, due)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			slog.Debug("finance procurement AP already exists", "documentRef", documentRef)
			return nil
		}
		return err
	}
	if h.bus != nil && h.bus.Enabled() {
		eventID := events.TypeInvoicePosted + ":" + documentRef
		h.bus.Publish(ctx, eventID, events.TypeInvoicePosted, map[string]any{
			"amount": amount, "currency": currency, "vendorRef": vendorRef, "documentRef": documentRef,
		}, documentRef)
	}
	slog.Info("finance AP item from procurement", "documentRef", documentRef, "id", item.ID)
	return nil
}

var errMissingProcurementFields = errors.New("procurement.invoice.received missing documentRef or amount")

// NewProcurement builds a consumer for procurement.invoice.received on iag.commercial.
func NewProcurement(cfg Config, ledgerSvc *ledger.Service, bus *events.Bus, dlq *platformevents.Producer) (*Consumer, error) {
	h := &procurementHandler{ledger: ledgerSvc, bus: bus}
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
