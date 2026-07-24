// Package antibot holds the abuse-resistance building blocks. M2 ships a
// keyed fixed-window rate limiter (magic-link requests per email and per
// IP); M4-T5 layers ALTCHA, honeypots and per-survey limits on top.
package antibot

import (
	"sync"
	"time"

	"github.com/TryEarful/earful/internal/clock"
)

// Limiter is an in-memory fixed-window rate limiter keyed by string
// (an email, an IP, a survey id...). In-memory is a deliberate MVP
// trade-off: the service runs as a single Cloud Run instance at MVP
// scale, and the sensitive limit (magic links per email) is additionally
// enforced against the database, which survives restarts.
type Limiter struct {
	limit  int
	window time.Duration
	clock  clock.Clock

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	start time.Time
	count int
}

// maxBuckets bounds memory: when exceeded, expired buckets are swept; if
// everything is live we still insert (correctness beats the bound — the
// window is short, so the map cannot grow unbounded in practice).
const maxBuckets = 100_000

func NewLimiter(limit int, window time.Duration, c clock.Clock) *Limiter {
	return &Limiter{
		limit:   limit,
		window:  window,
		clock:   c,
		buckets: make(map[string]*bucket),
	}
}

// Allow reports whether one more event for key fits the window, and
// counts it if so.
func (l *Limiter) Allow(key string) bool {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		if !ok && len(l.buckets) >= maxBuckets {
			l.sweep(now)
		}
		l.buckets[key] = &bucket{start: now, count: 1}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// sweep removes expired buckets. Caller holds l.mu.
func (l *Limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window {
			delete(l.buckets, k)
		}
	}
}
