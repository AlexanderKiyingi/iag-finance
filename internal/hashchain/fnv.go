package hashchain

import "fmt"

// EventHash matches the browser prototype in iag-finance.html (simpleHash).
func EventHash(ts, actor, eventType, message, prevHash string) string {
	return SimpleHash(ts + "|" + actor + "|" + eventType + "|" + message + "|" + prevHash)
}

func SimpleHash(input string) string {
	h := uint32(2166136261)
	for i := 0; i < len(input); i++ {
		h ^= uint32(input[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}
