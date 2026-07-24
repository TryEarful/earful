package clock_test

import (
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/clock"
)

func TestFake_AdvanceAndSet(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	c := clock.NewFake(start)

	if !c.Now().Equal(start) {
		t.Fatalf("Now() = %v, want %v", c.Now(), start)
	}
	c.Advance(90 * time.Minute)
	if want := start.Add(90 * time.Minute); !c.Now().Equal(want) {
		t.Errorf("after Advance: Now() = %v, want %v", c.Now(), want)
	}
	c.Set(start)
	if !c.Now().Equal(start) {
		t.Errorf("after Set: Now() = %v, want %v", c.Now(), start)
	}
}

func TestReal_MovesForward(t *testing.T) {
	t.Parallel()
	c := clock.Real{}
	first := c.Now()
	time.Sleep(time.Millisecond)
	if !c.Now().After(first) {
		t.Error("real clock did not advance")
	}
}
