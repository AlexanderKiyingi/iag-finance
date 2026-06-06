package events

import "testing"

func TestBusDisabledPublishNoPanic(t *testing.T) {
	var b *Bus
	b.Publish(t.Context(), "id", TypeSaleCompleted, map[string]any{"amount": "1"}, "key")
	b.PublishNotification(t.Context(), "a@b.com", "welcome-email", nil)
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
