package auth_test

import (
	"encoding/base64"
	"testing"

	"github.com/TryEarful/earful/internal/auth"
)

// TestNewToken_EntropyAndUniqueness: tokens must clear the ≥128-bit bar
// PLAN.md sets and never repeat.
func TestNewToken_EntropyAndUniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		tok := auth.NewToken()
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not base64url: %v", tok, err)
		}
		if len(raw)*8 < 128 {
			t.Fatalf("token carries %d bits, want ≥128", len(raw)*8)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = true
	}
}

// TestHashToken_StableAndIrreversible: the same token always hashes to
// the same 32 bytes, and different tokens do not collide — the property
// that lets the database store hashes instead of live credentials.
func TestHashToken_StableAndIrreversible(t *testing.T) {
	t.Parallel()

	tok := auth.NewToken()
	h1, h2 := auth.HashToken(tok), auth.HashToken(tok)
	if string(h1) != string(h2) {
		t.Error("hashing the same token twice produced different digests")
	}
	if len(h1) != 32 {
		t.Errorf("digest length = %d, want 32 (SHA-256)", len(h1))
	}
	if string(auth.HashToken(auth.NewToken())) == string(h1) {
		t.Error("distinct tokens hashed to the same digest")
	}
	if string(h1) == tok {
		t.Error("digest equals the raw token")
	}
}
