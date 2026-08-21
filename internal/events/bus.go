// Package events publishes finance domain events to iag.finance for the
// in-process (or peer) consumer to book journal entries.
package events

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"
)

var errNoProducer = errors.New("event producer not configured")

const (
	Source = "iag.finance"

	TypeSaleCompleted = "sale.completed"
	TypeInvoicePosted = "invoice.posted"

	TypeInvoiceApproved = "finance.invoice.approved"
	TypeEFRISSubmitted  = "finance.efris.submitted"
	TypePaymentMade     = "finance.payment.made"

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

// FinanceTopic is the configured Kafka topic for finance domain events. Used
// when enqueuing outbox rows so the relay can deliver them to the right topic.
func (b *Bus) FinanceTopic() string {
	if b == nil {
		return ""
	}
	return b.topic
}

// PublishRaw delivers a pre-built event to an explicit topic and RETURNS the
// error (unlike Publish, which is fire-and-forget). The outbox relay uses this
// so it can retry on failure instead of losing the event.
func (b *Bus) PublishRaw(ctx context.Context, topic, partitionKey, eventID, eventType string, payload map[string]any) error {
	if b == nil || b.producer == nil {
		return errNoProducer
	}
	env := platformevents.NewEnvelope(Source, eventType, payload)
	if eventID != "" {
		env.ID = eventID
	}
	if partitionKey == "" {
		partitionKey = env.ID
	}
	return b.producer.Publish(ctx, topic, partitionKey, env)
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

// NotificationEnvelope builds the notification.requested envelope. Split out so
// the idempotency key can be asserted without a broker: it is the one field
// whose mistakes are invisible at runtime, because a bad key makes
// iag-notifications answer "duplicate" and send nothing, successfully.
func NotificationEnvelope(eventID, recipient, templateID string, variables map[string]string) platformevents.Envelope {
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
	if strings.TrimSpace(eventID) != "" {
		env.ID = eventID
	}
	return env
}

// PublishNotification emits notification.requested on iag.notifications with a
// fresh idempotency key, so each call is a distinct notification.
//
// Prefer PublishNotificationID and pass a key derived from the thing being
// notified about: that makes a retry of the SAME notification collapse while
// still letting a genuinely new one through.
func (b *Bus) PublishNotification(ctx context.Context, recipient, templateID string, variables map[string]string) {
	b.PublishNotificationID(ctx, uuid.NewString(), recipient, templateID, variables)
}

// PublishNotificationID emits notification.requested with an explicit event id.
//
// That id is the idempotency key: iag-notifications dedups on
// (eventId, channel, recipient) and answers "duplicate" to a repeat, sending
// nothing. So the id MUST vary per real-world notification and repeat only for
// a retry of that same one.
//
// This previously derived the id from recipient+template alone, which is
// constant for a given pair. The first notification of a kind reached someone
// and every later one was silently discarded as a duplicate: a customer's
// second invoice email never arrived, and the daily overdue digest sent once
// and then never again. Nothing errored — dedup is a success path.
func (b *Bus) PublishNotificationID(ctx context.Context, eventID, recipient, templateID string, variables map[string]string) {
	if !b.NotificationsEnabled() || recipient == "" || templateID == "" {
		return
	}
	env := NotificationEnvelope(eventID, recipient, templateID, variables)
	if err := b.notificationProducer.Publish(ctx, b.notificationTopic, recipient, env); err != nil {
		slog.Warn("finance notification publish failed", "template", templateID, "err", err)
	}
}
