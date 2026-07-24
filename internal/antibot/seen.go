package antibot

import (
	"sync"
	"time"

	"github.com/TryEarful/earful/internal/clock"
)

// Seen is an in-memory set with expiry, used for single-use guarantees
// that only need to hold within one process lifetime: ALTCHA solutions
// (replay) and form nonces (double-click dedupe). In-memory is the same
// deliberate MVP trade-off as the rate limiter: one instance serves the
// MVP, and a restart forgiving a replay costs one duplicate row, not a
// broken invariant.
type Seen struct {
	ttl   time.Duration
	clock clock.Clock

	mu      sync.Mutex
	entries map[string]time.Time
}

func NewSeen(ttl time.Duration, c clock.Clock) *Seen {
	return &Seen{ttl: ttl, clock: c, entries: make(map[string]time.Time)}
}

// FirstUse records key and reports whether this was its first (unexpired)
// use.
func (s *Seen) FirstUse(key string) bool {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if expiry, ok := s.entries[key]; ok && now.Before(expiry) {
		return false
	}
	if len(s.entries) >= maxBuckets {
		for k, expiry := range s.entries {
			if !now.Before(expiry) {
				delete(s.entries, k)
			}
		}
	}
	s.entries[key] = now.Add(s.ttl)
	return true
}
