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

	TypeSaleCompleted = "sale.completed"
	TypeInvoicePosted = "invoice.posted"

	TypeInvoiceApproved = "finance.invoice.approved"
	TypeEFRISSubmitted  = "finance.efris.submitted"

	TypeNotificationRequested = "notification.requested"
	TopicNotifications        = "iag.notifications"
)

// Bus publishes to the configured finance Kafka topic.
type Bus struct {
	producer             *platformevents.Producer
	notificationProducer *platformevents.Producer
	topic                string
	notificationTopic    string
	financeEnabled       bool
}

// Config for the finance event producer.
type Config struct {
	Brokers           []string
	ClientID          string
	Topic             string
	NotificationTopic string
	// Enabled gates Publish on the finance ledger topic (sale.completed, etc.).
	// Notifications use NotificationsEnabled and work whenever brokers are configured.
	Enabled bool
}

// New builds a Bus. When brokers are empty, all publish methods are no-ops.
func New(cfg Config) *Bus {
	if len(cfg.Brokers) == 0 {
		return &Bus{}
	}
	prod := platformevents.NewProducer(platformevents.ProducerConfig{
		Brokers:  cfg.Brokers,
		ClientID: cfg.ClientID,
	})
	notifTopic := cfg.NotificationTopic
	if notifTopic == "" {
		notifTopic = TopicNotifications
	}
	return &Bus{
		producer:             prod,
		notificationProducer: prod,
		topic:                cfg.Topic,
		notificationTopic:    notifTopic,
		financeEnabled:       cfg.Enabled && cfg.Topic != "",
	}
}

// Enabled reports whether finance ledger event publishing is active.
func (b *Bus) Enabled() bool {
	return b != nil && b.financeEnabled && b.producer != nil
}

// NotificationsEnabled reports whether notification.requested can be published.
func (b *Bus) NotificationsEnabled() bool {
	return b != nil && b.producer != nil && b.notificationTopic != ""
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

// PublishNotification emits notification.requested on iag.notifications.
func (b *Bus) PublishNotification(ctx context.Context, recipient, templateID string, variables map[string]string) {
	if !b.NotificationsEnabled() || recipient == "" || templateID == "" {
		return
	}
	vars := map[string]any{}
	for k, v := range variables {
		vars[k] = v
	}
	env := platformevents.NewEnvelope(Source, TypeNotificationRequested, map[string]any{
		"channel":    "email",
		"recipient":  recipient,
		"templateId": templateID,
		"variables":  vars,
	})
	env.ID = TypeNotificationRequested + ":" + recipient + ":" + templateID
	if err := b.notificationProducer.Publish(ctx, b.notificationTopic, recipient, env); err != nil {
		slog.Warn("finance notification publish failed", "template", templateID, "err", err)
	}
}
