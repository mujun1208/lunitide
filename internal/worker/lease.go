// T-6.2.3: lease, heartbeat, Reaper and fencing tokens. A worker holds a
// lease with a fencing token; the token is globally monotonic. When the
// Reaper reclaims an expired lease it freezes the lease and rotates the
// fencing token, so a late result from the lost worker carries a token that
// no longer matches and is rejected (SBX-004). Reclamation happens on the
// next Reaper tick after expiry — the runtime binds the tick interval to the
// ≤60s budget.
package worker

import (
	"errors"
	"sync"
	"time"
)

var (
	// ErrLeaseExpired is returned when a heartbeat or result arrives after
	// the Reaper reclaimed the lease.
	ErrLeaseExpired = errors.New("worker: lease expired and reclaimed")
	// ErrLeaseNotFound covers unknown worker ids.
	ErrLeaseNotFound = errors.New("worker: lease not found")
	// ErrStaleFencing is SBX-004: the result carries a superseded fencing
	// token (the worker lost its lease; the Reaper already rotated).
	ErrStaleFencing = errors.New("worker: result rejected by stale fencing token")
)

// Lease is one worker lease. FencingToken is the token the worker must stamp
// on every result submission.
type Lease struct {
	WorkerID     string
	FencingToken uint64
	TTL          time.Duration
	HeartbeatAt  time.Duration
	ExpiresAt    time.Time
	Reclaimed    bool
}

// LeaseManager is the in-memory lease authority (the durable mirror lands
// with the task service; the fencing rules are pure and testable here).
type LeaseManager struct {
	mu       sync.Mutex
	now      func() time.Time
	next     uint64
	leases   map[string]*Lease
	rotated  map[string][]uint64 // workerID -> superseded tokens
	acquired map[string]uint64   // workerID -> live token
}

func NewLeaseManager() *LeaseManager {
	return &LeaseManager{
		now:      time.Now,
		leases:   map[string]*Lease{},
		rotated:  map[string][]uint64{},
		acquired: map[string]uint64{},
	}
}

// Acquire grants (or re-grants after reclamation) a lease and stamps a fresh
// fencing token. Tokens only ever increase.
func (m *LeaseManager) Acquire(workerID string, ttl time.Duration) Lease {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	l := &Lease{
		WorkerID: workerID, FencingToken: m.next, TTL: ttl,
		HeartbeatAt: ttl / 2, ExpiresAt: m.now().Add(ttl),
	}
	m.leases[workerID] = l
	m.acquired[workerID] = l.FencingToken
	return *l
}

// Heartbeat renews a live lease. A heartbeat after expiry (but before the
// Reaper tick) also fails: the lease is already past its TTL and the worker
// must not keep writing.
func (m *LeaseManager) Heartbeat(workerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[workerID]
	if !ok {
		return ErrLeaseNotFound
	}
	if l.Reclaimed || m.now().After(l.ExpiresAt) {
		return ErrLeaseExpired
	}
	l.ExpiresAt = m.now().Add(l.TTL)
	return nil
}

// ReaperTick reclaims every expired lease: it marks them Reclaimed, rotates
// the fencing token bookkeeping (old token recorded as superseded) and
// returns the reclaimed worker ids. The runtime calls this on an interval
// bounded by the ≤60s budget, so reclamation latency is at most one tick.
func (m *LeaseManager) ReaperTick() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var reclaimed []string
	now := m.now()
	for id, l := range m.leases {
		if l.Reclaimed || now.After(l.ExpiresAt) {
			if !l.Reclaimed {
				l.Reclaimed = true
				m.rotated[id] = append(m.rotated[id], l.FencingToken)
				delete(m.acquired, id)
				reclaimed = append(reclaimed, id)
			}
		}
	}
	return reclaimed
}

// SubmitResult is the fencing gate (SBX-004): the token must equal the live
// token for the worker. A superseded token (reclaimed + rotated) is rejected
// with ErrStaleFencing; an expired-but-not-yet-reaped lease is rejected with
// ErrLeaseExpired (the Reaper will rotate it on the next tick).
func (m *LeaseManager) SubmitResult(workerID string, token uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[workerID]
	if !ok {
		return ErrLeaseNotFound
	}
	if l.Reclaimed {
		return ErrStaleFencing
	}
	if m.now().After(l.ExpiresAt) {
		return ErrLeaseExpired
	}
	if token != l.FencingToken {
		return ErrStaleFencing
	}
	return nil
}
