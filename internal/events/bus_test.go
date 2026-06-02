package events

import "testing"

func TestBusDisabledPublishNoPanic(t *testing.T) {
	var b *Bus
	b.Publish(t.Context(), "id", TypeSaleCompleted, map[string]any{"amount": "1"}, "key")
}
