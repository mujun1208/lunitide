package worker

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTimedManager() (*LeaseManager, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	m := NewLeaseManager()
	m.now = clock.Now
	return m, clock
}

// Heartbeats keep the lease alive indefinitely; the fencing token stays
// stable across renewals.
func TestLeaseFencingHeartbeatRenews(t *testing.T) {
	m, clock := newTimedManager()
	lease := m.Acquire("w1", 30*time.Second)
	for i := 0; i < 10; i++ {
		clock.Advance(20 * time.Second)
		if err := m.Heartbeat("w1"); err != nil {
			t.Fatalf("heartbeat %d failed: %v", i, err)
		}
	}
	if err := m.SubmitResult("w1", lease.FencingToken); err != nil {
		t.Fatalf("live token result must be accepted, got %v", err)
	}
}

// A lost worker stops heartbeating; the Reaper reclaims on its tick and the
// late result carrying the old token is rejected (SBX-004).
func TestLeaseFencingLateResultRejected(t *testing.T) {
	m, clock := newTimedManager()
	lease := m.Acquire("w1", 30*time.Second)
	clock.Advance(45 * time.Second) // TTL gone, no heartbeats
	if err := m.Heartbeat("w1"); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("post-expiry heartbeat must fail, got %v", err)
	}
	if err := m.SubmitResult("w1", lease.FencingToken); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("post-expiry result must fail pre-reap, got %v", err)
	}
	reclaimed := m.ReaperTick()
	if len(reclaimed) != 1 || reclaimed[0] != "w1" {
		t.Fatalf("want w1 reclaimed, got %v", reclaimed)
	}
	if err := m.SubmitResult("w1", lease.FencingToken); !errors.Is(err, ErrStaleFencing) {
		t.Fatalf("late result with superseded token must be rejected (SBX-004), got %v", err)
	}
	// Re-acquire stamps a strictly greater token; the old one never
	// validates again.
	next := m.Acquire("w1", 30*time.Second)
	if next.FencingToken <= lease.FencingToken {
		t.Fatalf("token must be monotonic: %d then %d", lease.FencingToken, next.FencingToken)
	}
	if err := m.SubmitResult("w1", lease.FencingToken); !errors.Is(err, ErrStaleFencing) {
		t.Fatalf("superseded token must stay rejected, got %v", err)
	}
	if err := m.SubmitResult("w1", next.FencingToken); err != nil {
		t.Fatalf("fresh token must be accepted, got %v", err)
	}
}

// The Reaper bound: an expired lease is reclaimed on the very next tick, so
// a runtime ticking at (say) 15s reclaims within the 60s budget.
func TestLeaseFencingReaperLatency(t *testing.T) {
	m, clock := newTimedManager()
	m.Acquire("w1", 30*time.Second)
	clock.Advance(31 * time.Second)
	if got := m.ReaperTick(); len(got) != 1 {
		t.Fatalf("first tick after expiry must reclaim, got %v", got)
	}
	if got := m.ReaperTick(); len(got) != 0 {
		t.Fatalf("reclaimed leases must not be reclaimed twice, got %v", got)
	}
	// Unexpired leases survive the tick.
	m.Acquire("w2", time.Hour)
	clock.Advance(10 * time.Second)
	if got := m.ReaperTick(); len(got) != 0 {
		t.Fatalf("live lease must survive, got %v", got)
	}
	if err := m.SubmitResult("nope", 1); !errors.Is(err, ErrLeaseNotFound) {
		t.Fatalf("unknown worker must be LeaseNotFound, got %v", err)
	}
}

// Tokens are globally monotonic across different workers, so fencing
// survives worker replacement and interleaving.
func TestLeaseFencingTokensGloballyMonotonic(t *testing.T) {
	m, _ := newTimedManager()
	prev := uint64(0)
	for _, id := range []string{"a", "b", "a", "c", "b"} {
		l := m.Acquire(id, time.Minute)
		if l.FencingToken <= prev {
			t.Fatalf("token %d not greater than %d", l.FencingToken, prev)
		}
		prev = l.FencingToken
	}
}
