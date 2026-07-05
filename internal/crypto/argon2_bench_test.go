package crypto

import (
	"testing"
)

// BenchmarkOwnerSlotKDF measures the cost of one owner-slot password guess.
// THREAT_MODEL.md derives the attacker guess-rate budget from this number;
// re-run it (go test -bench OwnerSlotKDF ./internal/crypto) if the owner-slot
// Argon2id parameters change, and update the doc.
func BenchmarkOwnerSlotKDF(b *testing.B) {
	salt := []byte("0123456789abcdef")
	params := OwnerSlotParams()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := DeriveKey([]byte("benchmark password"), salt, params); err != nil {
			b.Fatal(err)
		}
	}
}
