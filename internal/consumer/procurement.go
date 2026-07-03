package consumer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

const (
	procurementInvoiceReceived = "procurement.invoice.received"
	procurementGrnPosted       = "procurement.grn.posted"
	// contractsPaymentAuthorized is emitted by iag-contract-management when a
	// milestone payment clears Payment Authorization — finance books the AP.
	contractsPaymentAuthorized = "contracts.payment.authorized"
)

type procurementHandler struct {
	ledger *ledger.Service
	bus    *events.Bus
}

func (h *procurementHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case procurementInvoiceReceived:
		return h.handleInvoiceReceived(ctx, env)
	case procurementGrnPosted:
		return h.handleGRNPosted(ctx, env)
	case contractsPaymentAuthorized:
		return h.handlePaymentAuthorized(ctx, env)
	default:
		return nil
	}
}

// handleGRNPosted accrues the AP liability when goods are received: it books the
// GR/IR accrual (Dr expense / Cr GR-IR clearing) for the received value carried
// on the event, keyed to the PO so the later invoice clears it. No-op for events
// without a PO ref or value (older emitters, or receipts with no priced lines).
func (h *procurementHandler) handleGRNPosted(ctx context.Context, env platformevents.Envelope) error {
	data := env.Data
	if data == nil {
		return nil
	}
	poRef, _ := data["po_id"].(string)
	poRef = strings.TrimSpace(poRef)
	amountStr, _ := data["amount"].(string)
	if poRef == "" || strings.TrimSpace(amountStr) == "" {
		return nil
	}
	value := parseAmount(amountStr)
	if value.IsZero() {
		return nil
	}
	currency, _ := data["currency"].(string)
	if strings.TrimSpace(currency) == "" {
		currency = "UGX"
	}
	// Optional received quantity for three-way qty matching (absent on older emitters).
	qty := decimal.Zero
	if q, ok := data["quantity"].(string); ok {
		qty = parseAmount(strings.TrimSpace(q))
	}
	if _, err := h.ledger.BookGRNAccrual(ctx, env.ID, env.Type, env.Source, env.CorrelationID, currency, poRef, value, qty); err != nil {
		return err
	}
	slog.Info("finance GR/IR accrual from GRN", "poRef", poRef, "amount", amountStr)
	return nil
}

// handlePaymentAuthorized books an AP open item for an authorized contract
// milestone payment. documentRef is the payment id (idempotent in finance).
func (h *procurementHandler) handlePaymentAuthorized(ctx context.Context, env platformevents.Envelope) error {
	data := env.Data
	if data == nil {
		return nil
	}
	paymentID, _ := data["paymentId"].(string)
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return nil
	}
	payable, _ := data["payable"].(float64)
	if payable <= 0 {
		return nil
	}
	vendorRef, _ := data["contractor"].(string)
	if strings.TrimSpace(vendorRef) == "" {
		vendorRef = "contract-management"
	}
	number, _ := data["contractNumber"].(string)
	documentRef := "CT-PAY-" + paymentID
	// Keep cents: int64(payable) truncated the fractional currency unit.
	amount := decimal.NewFromFloat(payable).StringFixed(2)
	desc := strings.TrimSpace("Contract milestone payment " + number)
	item, err := h.ledger.CreateAPItem(ctx, vendorRef, documentRef, desc, amount, "UGX", nil, nil)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			slog.Debug("finance contract payment AP already exists", "documentRef", documentRef)
			return nil
		}
		return err
	}
	slog.Info("finance AP item from contract payment", "documentRef", documentRef, "id", item.ID)
	return nil
}

// invoicePostedOutbox builds the invoice.posted outbox event for the consumer,
// or nil when publishing is disabled. Written atomically with the AP item.
func (h *procurementHandler) invoicePostedOutbox(documentRef, vendorRef, amount, currency, poRef, vatAmount, taxCode, quantity string, reverseCharge bool) *repository.OutboxEvent {
	if h.bus == nil || !h.bus.Enabled() {
		return nil
	}
	payload := map[string]any{
		"amount": amount, "currency": currency, "vendorRef": vendorRef, "documentRef": documentRef,
	}
	// Carry the PO ref and any VAT through to the GL-booking handler so it can
	// clear a GR/IR accrual and split input VAT.
	if poRef != "" {
		payload["poRef"] = poRef
	}
	if vatAmount != "" {
		payload["vatAmount"] = vatAmount
	}
	// Carry the invoiced quantity for three-way qty matching.
	if quantity != "" {
		payload["quantity"] = quantity
	}
	// Carry reverse-charge intent so the GL handler self-assesses VAT on the net.
	if reverseCharge {
		payload["reverseCharge"] = true
		if taxCode != "" {
			payload["taxCode"] = taxCode
		}
	}
	return &repository.OutboxEvent{
		Topic:        h.bus.FinanceTopic(),
		PartitionKey: documentRef,
		EventID:      events.TypeInvoicePosted + ":" + documentRef,
		EventType:    events.TypeInvoicePosted,
		Payload:      payload,
	}
}

func (h *procurementHandler) handleInvoiceReceived(ctx context.Context, env platformevents.Envelope) error {
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
	poRef, _ := data["poRef"].(string)
	poRef = strings.TrimSpace(poRef)
	vatAmount, _ := data["vatAmount"].(string)
	vatAmount = strings.TrimSpace(vatAmount)
	taxCode, _ := data["taxCode"].(string)
	taxCode = strings.TrimSpace(taxCode)
	reverseCharge, _ := data["reverseCharge"].(bool)
	quantity, _ := data["quantity"].(string)
	quantity = strings.TrimSpace(quantity)
	// invoice.posted is enqueued to the outbox in the same tx as the AP item.
	outbox := h.invoicePostedOutbox(documentRef, vendorRef, amount, currency, poRef, vatAmount, taxCode, quantity, reverseCharge)
	item, err := h.ledger.CreateAPItem(ctx, vendorRef, documentRef, desc, amount, currency, due, outbox)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			slog.Debug("finance procurement AP already exists", "documentRef", documentRef)
			return nil
		}
		return err
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
