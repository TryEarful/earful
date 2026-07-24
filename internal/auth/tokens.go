package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewToken returns a fresh 256-bit random token, base64url-encoded. Used
// for session cookies, magic links, and CSRF tokens. 256 bits comfortably
// clears the ≥128-bit guessing-infeasibility bar PLAN.md sets for tokens.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms; if it somehow
		// does, issuing predictable credentials is not an option.
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is the at-rest form of a token: the database stores only
// SHA-256(token), so a database leak cannot be replayed into live
// sessions or unconsumed magic links. SHA-256 (not a slow KDF) is right
// here: the input is 256 random bits, not a guessable password.
func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
