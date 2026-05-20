package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/auditlog"
	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/ledger"
)

type Config struct {
	Brokers []string
	GroupID string
	Topic   string
}

type PlatformEvent struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Time          string          `json:"time"`
	Source        string          `json:"source"`
	SpecVersion   string          `json:"specversion"`
	CorrelationID string          `json:"correlationId"`
	CausationID   string          `json:"causationId"`
	Data          json.RawMessage `json:"data"`
}

type saleCompletedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	CustomerRef string `json:"customerRef"`
	DocumentRef string `json:"documentRef"`
}

type invoicePostedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	VendorRef   string `json:"vendorRef"`
	DocumentRef string `json:"documentRef"`
}

type fleetFuelRecordedData struct {
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	VendorRef   string `json:"vendorRef"`
	DocumentRef string `json:"documentRef"`
	VehicleID   string `json:"vehicleId"`
	Litres      string `json:"litres"`
}

type Consumer struct {
	reader  *kafka.Reader
	ledger  *ledger.Service
	audit   *auditlog.Service
}

func New(cfg Config, ledgerSvc *ledger.Service, auditSvc *auditlog.Service) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  cfg.Brokers,
		GroupID:  cfg.GroupID,
		Topic:    cfg.Topic,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	return &Consumer{reader: reader, ledger: ledgerSvc, audit: auditSvc}
}

func (c *Consumer) Run(ctx context.Context) error {
	slog.Info("finance consumer started", "topic", c.reader.Config().Topic)
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("kafka fetch", "error", err)
			time.Sleep(time.Second)
			continue
		}

		if err := c.handleMessage(ctx, msg.Value); err != nil {
			slog.Error("handle finance event", "error", err)
		} else if err := c.reader.CommitMessages(ctx, msg); err != nil {
			slog.Error("kafka commit", "error", err)
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}

func (c *Consumer) handleMessage(ctx context.Context, payload []byte) error {
	var evt PlatformEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return err
	}
	if evt.ID == "" || evt.Type == "" {
		return nil
	}

	switch evt.Type {
	case "sale.completed":
		return c.handleSaleCompleted(ctx, evt)
	case "invoice.posted":
		return c.handleInvoicePosted(ctx, evt)
	case "fleet.fuel.recorded":
		return c.handleFleetFuelRecorded(ctx, evt)
	default:
		slog.Debug("ignored finance event", "type", evt.Type)
		return nil
	}
}

func (c *Consumer) handleSaleCompleted(ctx context.Context, evt PlatformEvent) error {
	var data saleCompletedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return err
	}
	amount, err := decimal.NewFromString(data.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		amount = decimal.NewFromInt(0)
	}
	if amount.IsZero() {
		return nil
	}

	desc := "Sale completed"
	if data.DocumentRef != "" {
		desc += " — " + data.DocumentRef
	}

	entry, err := c.ledger.BookFromEvent(ctx, evt.ID, evt.Type, evt.Source, evt.CorrelationID, desc, []ledger.LineInput{
		{AccountCode: "1100", Debit: amount, Memo: "AR from sale"},
		{AccountCode: "4000", Credit: amount, Memo: "Revenue"},
	})
	if err == nil {
		c.logBookedEvent(ctx, evt, entry)
	}
	return err
}

func (c *Consumer) handleInvoicePosted(ctx context.Context, evt PlatformEvent) error {
	var data invoicePostedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return err
	}
	amount, err := decimal.NewFromString(data.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		amount = decimal.NewFromInt(0)
	}
	if amount.IsZero() {
		return nil
	}

	desc := "Invoice posted"
	if data.DocumentRef != "" {
		desc += " — " + data.DocumentRef
	}

	entry, err := c.ledger.BookFromEvent(ctx, evt.ID, evt.Type, evt.Source, evt.CorrelationID, desc, []ledger.LineInput{
		{AccountCode: "5000", Debit: amount, Memo: "Expense / COGS"},
		{AccountCode: "2000", Credit: amount, Memo: "AP liability"},
	})
	if err == nil {
		c.logBookedEvent(ctx, evt, entry)
	}
	return err
}

func (c *Consumer) handleFleetFuelRecorded(ctx context.Context, evt PlatformEvent) error {
	var data fleetFuelRecordedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return err
	}
	amount, err := decimal.NewFromString(data.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		amount = decimal.NewFromInt(0)
	}
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

	entry, err := c.ledger.BookFromEvent(ctx, evt.ID, evt.Type, evt.Source, evt.CorrelationID, desc, []ledger.LineInput{
		{AccountCode: "5000", Debit: amount, Memo: "Fleet fuel expense"},
		{AccountCode: "2000", Credit: amount, Memo: "AP / fuel payable"},
	})
	if err == nil {
		c.logBookedEvent(ctx, evt, entry)
	}
	return err
}

func (c *Consumer) logBookedEvent(ctx context.Context, evt PlatformEvent, entry *domain.JournalEntry) {
	if c.audit == nil || entry == nil {
		return
	}
	_ = c.audit.Record(ctx, auditlog.RecordInput{
		EventType:    auditlog.EventJournalBookedEvent,
		ActorEmail:   "system",
		ResourceType: "journal_entry",
		ResourceID:   entry.ID.String(),
		CorrelationID: evt.CorrelationID,
		Metadata: map[string]any{
			"sourceEventId":   evt.ID,
			"sourceEventType": evt.Type,
			"sourceService":   evt.Source,
			"entryNumber":     entry.EntryNumber,
		},
	})
}
