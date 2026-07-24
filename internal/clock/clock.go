// Package clock provides the injectable clock the whole application tells
// time through (SPEC.md Testing Decisions): magic-link and session expiry,
// close dates, purge windows, insight cache watermarks. Production code
// receives a Clock; tests inject a Fake and time-travel.
package clock

import (
	"sync"
	"time"
)

// Clock is the single source of "now" for application logic. Database
// created_at defaults still use the database's own now() — they are
// informational; every expiry or window comparison goes through Clock.
type Clock interface {
	Now() time.Time
}

// Real is the wall clock used outside tests.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Fake is a settable clock for tests. The zero value is not useful; use
// NewFake.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake frozen at t (or the current time if t is zero).
func NewFake(t time.Time) *Fake {
	if t.IsZero() {
		t = time.Now()
	}
	return &Fake{now: t}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the fake clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}
