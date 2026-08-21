package events

import (
	"context"
	"testing"
)

func TestBusDisabledPublishNoPanic(t *testing.T) {
	var b *Bus
	b.Publish(context.Background(), "id", TypeSaleCompleted, map[string]any{"amount": "1"}, "key")
	b.PublishNotification(context.Background(), "a@b.com", "welcome-email", nil)
}

// The envelope id is the idempotency key iag-notifications dedups on
// (eventId, channel, recipient). It was once derived from recipient+template
// alone — constant for a given pair — so the first notification of a kind was
// delivered and every later one was discarded as a duplicate. Dedup is a
// success path: nothing errored, nothing logged, the mail simply stopped.
func TestNotificationEnvelopeIDIsNotConstantPerRecipientAndTemplate(t *testing.T) {
	const to, tmpl = "ap@iag.local", "invoice-ready-email"

	a := NotificationEnvelope("", to, tmpl, map[string]string{"documentRef": "INV-1"})
	b := NotificationEnvelope("", to, tmpl, map[string]string{"documentRef": "INV-2"})
	if a.ID == b.ID {
		t.Errorf("two notifications to the same recipient on the same template share id %q; "+
			"the second would be dropped as a duplicate", a.ID)
	}

	// An explicit key is honoured verbatim, so a genuine retry of the SAME
	// notification still collapses.
	key := "invoice-ready:INV-1:" + to
	if got := NotificationEnvelope(key, to, tmpl, nil); got.ID != key {
		t.Errorf("explicit event id = %q, want %q", got.ID, key)
	}
	if first, second := NotificationEnvelope(key, to, tmpl, nil), NotificationEnvelope(key, to, tmpl, nil); first.ID != second.ID {
		t.Error("the same explicit key must produce the same id so retries dedup")
	}
}

func TestNotificationEnvelopeCarriesChannelAndTemplate(t *testing.T) {
	env := NotificationEnvelope("k", "a@b.com", "approval.decision", map[string]string{"Title": "T"})
	if env.Data["channel"] != "email" {
		t.Errorf("channel = %v", env.Data["channel"])
	}
	if env.Data["templateId"] != "approval.decision" {
		t.Errorf("templateId = %v", env.Data["templateId"])
	}
	if env.Type != TypeNotificationRequested {
		t.Errorf("type = %q", env.Type)
	}
	vars, ok := env.Data["variables"].(map[string]any)
	if !ok || vars["Title"] != "T" {
		t.Errorf("variables = %v", env.Data["variables"])
	}
}

func TestNotificationsEnabledWithoutFinanceTopic(t *testing.T) {
	b := New(Config{
		Brokers:           []string{"localhost:9092"},
		ClientID:          "test",
		Topic:             "iag.finance",
		NotificationTopic: "iag.notifications",
		Enabled:           false,
	})
	if !b.NotificationsEnabled() {
		t.Fatal("expected notifications enabled when brokers configured")
	}
	if b.Enabled() {
		t.Fatal("finance ledger publish should stay disabled")
	}
}
