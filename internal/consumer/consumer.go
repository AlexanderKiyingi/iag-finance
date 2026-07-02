package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/ledger"
)

// Config controls one Kafka subscription.
type Config struct {
	Brokers []string
	GroupID string
	Topic   string
	// DLQTopic receives messages whose handler returned a Permanent error or
	// exceeded retries. Empty disables DLQ.
	DLQTopic string
}

// Consumer wraps a platform-go events.Consumer with finance-specific handlers.
// Dedupe stays inside ledger.BookFromEvent (IsEventProcessed/MarkEventProcessed
// against the existing finance processed_events table), so the platform-go
// consumer uses NoopDedupe — DLQ + retry come from platform-go, idempotency
// comes from the ledger.
type Consumer struct {
	inner *platformevents.Consumer
}

// New builds a Consumer that publishes its DLQ via the supplied producer (may
// be nil to disable DLQ).
func New(cfg Config, ledgerSvc *ledger.Service, auditSvc *auditlog.Service, dlq *platformevents.Producer) (*Consumer, error) {
	h := &financeHandler{ledger: ledgerSvc, audit: auditSvc}
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

// Run blocks consuming until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error { return c.inner.Run(ctx) }

// Close shuts down the underlying reader.
func (c *Consumer) Close() error { return c.inner.Close() }

// financeHandler dispatches on event type. Unknown types are ignored (return
// nil); decode failures are returned as Permanent so the message goes to DLQ
// instead of looping.
type financeHandler struct {
	ledger *ledger.Service
	audit  *auditlog.Service
}

func (h *financeHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case "sale.completed":
		return h.handleSaleCompleted(ctx, env)
	case "invoice.posted":
		return h.handleInvoicePosted(ctx, env)
	case "fleet.fuel.recorded":
		return h.handleFleetFuelRecorded(ctx, env)
	default:
		slog.Debug("finance ignoring event", "type", env.Type, "topic_envelope_source", env.Source)
		return nil
	}
}

type saleCompletedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	CustomerRef string `json:"customerRef"`
	DocumentRef string `json:"documentRef"`
	// VatAmount, when present, is the VAT portion already included in Amount; the
	// booking splits it to the VAT control account (output VAT). Absent → no split.
	VatAmount string `json:"vatAmount"`
}

type invoicePostedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	VendorRef   string `json:"vendorRef"`
	DocumentRef string `json:"documentRef"`
	// PoRef lets the booking clear a GR/IR accrual raised at goods receipt.
	// VatAmount, when present, is the VAT portion already included in Amount.
	PoRef     string `json:"poRef"`
	VatAmount string `json:"vatAmount"`
	// ReverseCharge + TaxCode drive buyer self-assessed VAT (supplier bills none):
	// the AP is the net, and net × rate is booked as offsetting input/output VAT.
	ReverseCharge bool   `json:"reverseCharge"`
	TaxCode       string `json:"taxCode"`
}

type fleetFuelRecordedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	VendorRef   string `json:"vendorRef"`
	DocumentRef string `json:"documentRef"`
	VehicleID   string `json:"vehicleId"`
	Litres      string `json:"litres"`
}

func (h *financeHandler) handleSaleCompleted(ctx context.Context, env platformevents.Envelope) error {
	var data saleCompletedData
	if err := remarshal(env.Data, &data); err != nil {
		return platformevents.Permanent(err)
	}
	amount := parseAmount(data.Amount)
	if amount.IsZero() {
		return nil
	}
	desc := "Sale completed"
	if data.DocumentRef != "" {
		desc += " — " + data.DocumentRef
	}
	// Split out output VAT only when the event carries a VAT amount; otherwise the
	// full amount is revenue (single-line credit, unchanged behaviour).
	vat := parseAmount(data.VatAmount)
	lines := []ledger.LineInput{{AccountCode: "1100", Debit: amount, Memo: "AR from sale"}}
	if vat.IsPositive() && vat.LessThan(amount) {
		lines = append(lines,
			ledger.LineInput{AccountCode: "4000", Credit: amount.Sub(vat), Memo: "Revenue"},
			ledger.LineInput{AccountCode: "2100", Credit: vat, Memo: "Output VAT"},
		)
	} else {
		lines = append(lines, ledger.LineInput{AccountCode: "4000", Credit: amount, Memo: "Revenue"})
	}
	entry, err := h.ledger.BookFromEvent(ctx, env.ID, env.Type, env.Source, env.CorrelationID, desc, data.Currency, lines)
	if err == nil {
		h.logBooked(ctx, env, entry)
		h.linkOpenItem(ctx, env.Type, data.DocumentRef, entry, env.ID)
	}
	return err
}

func (h *financeHandler) handleInvoicePosted(ctx context.Context, env platformevents.Envelope) error {
	var data invoicePostedData
	if err := remarshal(env.Data, &data); err != nil {
		return platformevents.Permanent(err)
	}
	amount := parseAmount(data.Amount)
	if amount.IsZero() {
		return nil
	}
	desc := "Invoice posted"
	if data.DocumentRef != "" {
		desc += " — " + data.DocumentRef
	}
	// Books AP, clearing any GR/IR accrual for the PO and splitting VAT when the
	// event carries it. poRef "" + vat 0 reduces to the prior Dr 5000 / Cr 2000.
	entry, err := h.ledger.BookAPInvoice(ctx, env.ID, env.Type, env.Source, env.CorrelationID, desc,
		data.Currency, strings.TrimSpace(data.PoRef), amount, parseAmount(data.VatAmount),
		data.ReverseCharge, strings.TrimSpace(data.TaxCode))
	if err == nil {
		h.logBooked(ctx, env, entry)
		h.linkOpenItem(ctx, env.Type, data.DocumentRef, entry, env.ID)
	}
	return err
}

func (h *financeHandler) handleFleetFuelRecorded(ctx context.Context, env platformevents.Envelope) error {
	var data fleetFuelRecordedData
	if err := remarshal(env.Data, &data); err != nil {
		return platformevents.Permanent(err)
	}
	amount := parseAmount(data.Amount)
	if amount.IsZero() {
		return nil
	}
	desc := "Fleet fuel purchase"
	if data.DocumentRef != "" {
		desc += " — " + data.DocumentRef
	}
	if data.VehicleID != "" {
		desc += " (" + data.VehicleID + ")"
	}
	entry, err := h.ledger.BookFromEvent(ctx, env.ID, env.Type, env.Source, env.CorrelationID, desc, data.Currency, []ledger.LineInput{
		{AccountCode: "5000", Debit: amount, Memo: "Fleet fuel expense"},
		{AccountCode: "2000", Credit: amount, Memo: "AP / fuel payable"},
	})
	if err == nil {
		h.logBooked(ctx, env, entry)
		h.linkOpenItem(ctx, env.Type, data.DocumentRef, entry, env.ID)
	}
	return err
}

func (h *financeHandler) linkOpenItem(ctx context.Context, eventType, documentRef string, entry *domain.JournalEntry, eventID string) {
	if entry == nil || documentRef == "" {
		return
	}
	switch eventType {
	case "sale.completed":
		if err := h.ledger.LinkARToJournal(ctx, documentRef, entry.ID, eventID); err != nil {
			slog.Warn("finance AR link failed", "documentRef", documentRef, "err", err)
		}
	case "invoice.posted", "fleet.fuel.recorded":
		if err := h.ledger.LinkAPToJournal(ctx, documentRef, entry.ID, eventID); err != nil {
			slog.Warn("finance AP link failed", "documentRef", documentRef, "err", err)
		}
	}
}

func (h *financeHandler) logBooked(ctx context.Context, env platformevents.Envelope, entry *domain.JournalEntry) {
	if h.audit == nil || entry == nil {
		return
	}
	_ = h.audit.Record(ctx, auditlog.RecordInput{
		EventType:     auditlog.EventJournalBookedEvent,
		ActorEmail:    "system",
		ResourceType:  "journal_entry",
		ResourceID:    entry.ID.String(),
		CorrelationID: env.CorrelationID,
		Metadata: map[string]any{
			"sourceEventId":   env.ID,
			"sourceEventType": env.Type,
			"sourceService":   env.Source,
			"entryNumber":     entry.EntryNumber,
		},
	})
}

// remarshal turns the generic map[string]any in env.Data into a typed struct.
// platform-go's Envelope holds Data as map[string]any (JSON decoded once);
// the original consumer worked off raw bytes, so we round-trip via JSON.
func remarshal(in map[string]any, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func parseAmount(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil || d.LessThanOrEqual(decimal.Zero) {
		return decimal.NewFromInt(0)
	}
	return d
}
