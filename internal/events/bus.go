// Package events publishes finance domain events to iag.finance for the
// in-process (or peer) consumer to book journal entries.
package events

import (
	"context"
	"log/slog"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
)

const (
	Source = "iag.finance"

	TypeSaleCompleted  = "sale.completed"
	TypeInvoicePosted  = "invoice.posted"
)

// Bus publishes to the configured finance Kafka topic.
type Bus struct {
	producer *platformevents.Producer
	topic    string
}

// Config for the finance event producer.
type Config struct {
	Brokers  []string
	ClientID string
	Topic    string
	Enabled  bool
}

// New builds a Bus. When disabled, Publish is a no-op.
func New(cfg Config) *Bus {
	if !cfg.Enabled || len(cfg.Brokers) == 0 {
		return &Bus{}
	}
	return &Bus{
		producer: platformevents.NewProducer(platformevents.ProducerConfig{
			Brokers:  cfg.Brokers,
			ClientID: cfg.ClientID,
		}),
		topic: cfg.Topic,
	}
}

// Enabled reports whether publishing is active.
func (b *Bus) Enabled() bool {
	return b != nil && b.producer != nil && b.topic != ""
}

// Close shuts down the producer.
func (b *Bus) Close() error {
	if b == nil || b.producer == nil {
		return nil
	}
	return b.producer.Close()
}

// Publish emits an envelope on iag.finance. eventID should be stable per
// document (e.g. sale.completed:DOC-1) so the ledger consumer stays idempotent.
func (b *Bus) Publish(ctx context.Context, eventID, eventType string, data map[string]any, partitionKey string) {
	if !b.Enabled() {
		return
	}
	env := platformevents.NewEnvelope(Source, eventType, data)
	if eventID != "" {
		env.ID = eventID
	}
	if partitionKey == "" {
		partitionKey = env.ID
	}
	if err := b.producer.Publish(ctx, b.topic, partitionKey, env); err != nil {
		slog.Warn("finance event publish failed", "type", eventType, "id", env.ID, "err", err)
	}
}
