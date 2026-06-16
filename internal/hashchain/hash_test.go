package hashchain

import "testing"

func TestSimpleHashIsSHA256(t *testing.T) {
	// Deterministic and 64 hex chars (SHA-256), not the old 8-char FNV digest.
	h := SimpleHash("hello")
	if len(h) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %d chars: %s", len(h), h)
	}
	if SimpleHash("hello") != h {
		t.Fatal("hash is not deterministic")
	}
	// Known SHA-256("hello").
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if h != want {
		t.Fatalf("unexpected sha256: got %s want %s", h, want)
	}
}

func TestEventHashLinkageSensitivity(t *testing.T) {
	base := EventHash("2026-06-15T00:00:00Z", "alice", "ledger.posted", "JE-1", "GENESIS")

	// Any field change must change the hash (tamper-evidence).
	cases := map[string]string{
		"actor":   EventHash("2026-06-15T00:00:00Z", "mallory", "ledger.posted", "JE-1", "GENESIS"),
		"type":    EventHash("2026-06-15T00:00:00Z", "alice", "ledger.reversed", "JE-1", "GENESIS"),
		"message": EventHash("2026-06-15T00:00:00Z", "alice", "ledger.posted", "JE-2", "GENESIS"),
		"prev":    EventHash("2026-06-15T00:00:00Z", "alice", "ledger.posted", "JE-1", "abc123"),
		"ts":      EventHash("2026-06-15T00:00:01Z", "alice", "ledger.posted", "JE-1", "GENESIS"),
	}
	for field, h := range cases {
		if h == base {
			t.Fatalf("changing %s did not change the event hash", field)
		}
	}
}
