package hashchain

import (
	"crypto/sha256"
	"encoding/hex"
)

// EventHash links one audit event to its predecessor. The chain is tamper-
// evident: changing any field of any past event changes its hash, which breaks
// every subsequent prev_hash linkage (see chainaudit.Verify).
func EventHash(ts, actor, eventType, message, prevHash string) string {
	return SimpleHash(ts + "|" + actor + "|" + eventType + "|" + message + "|" + prevHash)
}

// SimpleHash is a SHA-256 hex digest. It replaced a 32-bit FNV-1a digest that
// was trivially forgeable and collision-prone — unacceptable for an audit
// trail. The name is retained for call-site compatibility.
func SimpleHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
