package event

import (
	"testing"
	"time"
)

func newFireController() *controller {
	return &controller{fireTimes: make(map[int64][]time.Time)}
}

func TestAllowFire_BurstThenBlock(t *testing.T) {
	c := newFireController()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < eventFireBurst; i++ {
		if !c.allowFire(1, now) {
			t.Fatalf("fire %d within burst should be allowed", i)
		}
	}
	if c.allowFire(1, now) {
		t.Fatal("fire beyond the burst budget must be blocked (breaks runaway chains)")
	}
}

func TestAllowFire_WindowResets(t *testing.T) {
	c := newFireController()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < eventFireBurst; i++ {
		c.allowFire(1, now)
	}
	if c.allowFire(1, now) {
		t.Fatal("should be blocked while the window is saturated")
	}

	later := now.Add(eventFireWindow + time.Second)
	if !c.allowFire(1, later) {
		t.Fatal("should be allowed again once the window has elapsed")
	}
}

func TestAllowFire_IndependentPerEvent(t *testing.T) {
	c := newFireController()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < eventFireBurst; i++ {
		c.allowFire(1, now)
	}
	if !c.allowFire(2, now) {
		t.Fatal("a different event must have its own independent budget")
	}
}

// Pruning must keep the sliding window bounded: old timestamps drop out so a steady,
// under-cap rate never trips the breaker.
func TestAllowFire_SteadyRateNeverBlocks(t *testing.T) {
	c := newFireController()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// One fire every half-window for many iterations: always well under the burst.
	step := eventFireWindow / 2
	for i := 0; i < 100; i++ {
		if !c.allowFire(1, now) {
			t.Fatalf("steady under-cap fire %d should be allowed", i)
		}
		now = now.Add(step)
	}
	if got := len(c.fireTimes[1]); got > eventFireBurst {
		t.Fatalf("window not pruned: %d timestamps retained", got)
	}
}
