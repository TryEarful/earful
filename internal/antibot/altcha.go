package antibot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go"

	"github.com/TryEarful/earful/internal/clock"
)

// Challenges wraps ALTCHA proof-of-work (ADR-0006): the server mints
// challenges, the browser burns CPU solving them, and each solution is
// single-use. The client side is a ~40-line first-party solver in
// respond.js rather than the upstream widget — same wire protocol, no
// vendored bundle, and no blob: worker to carve out of the CSP.
//
// The HMAC key is random per process, like FormTokens: challenges live
// for minutes, so key rotation at deploy time costs nothing.
type Challenges struct {
	key  string
	seen *Seen
}

// challengeMaxNumber tunes solve cost. ~50k awaited WebCrypto digests is
// noticeable work for a bulk submitter but sub-second on any phone.
const challengeMaxNumber = 50_000

// challengeTTL is how long a minted challenge stays solvable. Long enough
// to answer a survey slowly; the solution itself is verified against this
// via the expires baked into the salt.
const challengeTTL = 2 * time.Hour

var ErrChallengeFailed = errors.New("antibot: challenge verification failed")

func NewChallenges(c clock.Clock) *Challenges {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("antibot: crypto/rand failed: " + err.Error())
	}
	return &Challenges{
		key: hex.EncodeToString(raw),
		// Replay window comfortably outlives challenge validity.
		seen: NewSeen(3*time.Hour, c),
	}
}

// New mints a challenge for the client to solve.
func (c *Challenges) New() (altcha.Challenge, error) {
	expires := time.Now().Add(challengeTTL)
	return altcha.CreateChallenge(altcha.ChallengeOptions{
		HMACKey:   c.key,
		MaxNumber: challengeMaxNumber,
		Expires:   &expires,
	})
}

// Verify checks a base64 solution payload and burns it: a valid solution
// works exactly once.
func (c *Challenges) Verify(payload string) error {
	ok, err := altcha.VerifySolution(payload, c.key, true)
	if err != nil || !ok {
		return ErrChallengeFailed
	}
	if !c.seen.FirstUse(payload) {
		return ErrChallengeFailed
	}
	return nil
}
