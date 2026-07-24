package antibot_test

import (
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/clock"
)

func TestLimiter_AllowsUpToLimitThenRefuses(t *testing.T) {
	t.Parallel()
	c := clock.NewFake(time.Now())
	l := antibot.NewLimiter(3, time.Minute, c)

	for i := 1; i <= 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("event %d refused, want allowed", i)
		}
	}
	if l.Allow("k") {
		t.Error("4th event allowed, want refused")
	}
}

func TestLimiter_KeysAreIndependent(t *testing.T) {
	t.Parallel()
	c := clock.NewFake(time.Now())
	l := antibot.NewLimiter(1, time.Minute, c)

	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("first event for each key should be allowed")
	}
	if l.Allow("a") {
		t.Error("second event for key a allowed, want refused")
	}
}

func TestLimiter_WindowResets(t *testing.T) {
	t.Parallel()
	c := clock.NewFake(time.Now())
	l := antibot.NewLimiter(2, time.Minute, c)

	l.Allow("k")
	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("third event within the window should be refused")
	}

	c.Advance(61 * time.Second)
	if !l.Allow("k") {
		t.Error("event after the window elapsed should be allowed")
	}
}
