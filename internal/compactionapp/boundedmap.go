package compactionapp

import (
	"container/list"
	"sync"
	"time"
)

// Q-04 (compaction trigger memory leak): the Trigger previously kept two
// per-session maps that only ever grew:
//
//   - lastTrigger map[string]time.Time  (cooldown timestamps, never evicted
//     except by ResetCooldown)
//   - sessionLocks sync.Map              (one channel latch per session, never
//     deleted)
//
// A long-running process that serves an unbounded number of distinct sessions
// would therefore leak memory monotonically. The structures below replace both
// maps with bounded ones. Semantics that matter for correctness (cooldown
// timing, same-session serialization, cancellation-aware waiting) are preserved
// exactly; only the underlying container is made bounded.

const (
	// lastTriggerMaxEntries caps the number of cooldown timestamps retained.
	// Matches PRD Q-04. When exceeded, the least-recently-used session's
	// cooldown is forgotten (worst case: that idle session may re-trigger one
	// cycle earlier, which is harmless).
	lastTriggerMaxEntries = 10000

	// sessionLockShards is the fixed number of channel latches used to
	// serialize compaction on a session. A power of two keeps the modulo
	// cheap. Distinct sessions that hash to the same shard serialize against
	// each other (a benign over-serialization); same-session serialization is
	// always guaranteed and there is no per-session allocation to leak.
	sessionLockShards = 1024
)

// lruTimeMap is a bounded, concurrency-safe key -> time.Time map with
// least-recently-used eviction. It carries its own mutex, so callers must NOT
// wrap it in an additional lock (that would risk a double-lock/deadlock).
type lruTimeMap struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List               // front = most recently used
	items    map[string]*list.Element // key -> element holding *lruTimeEntry
}

type lruTimeEntry struct {
	key string
	val time.Time
}

// newLRUTimeMap creates a bounded LRU map. A non-positive capacity is treated
// as 1 to avoid a degenerate (unbounded or panicking) structure.
func newLRUTimeMap(capacity int) *lruTimeMap {
	if capacity <= 0 {
		capacity = 1
	}
	return &lruTimeMap{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

// Get returns the stored time for key and marks it most-recently-used.
func (m *lruTimeMap) Get(key string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.ll.MoveToFront(el)
		return el.Value.(*lruTimeEntry).val, true
	}
	return time.Time{}, false
}

// Set stores val for key (updating in place if present) and evicts the
// least-recently-used entry when the capacity is exceeded.
func (m *lruTimeMap) Set(key string, val time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		el.Value.(*lruTimeEntry).val = val
		m.ll.MoveToFront(el)
		return
	}
	el := m.ll.PushFront(&lruTimeEntry{key: key, val: val})
	m.items[key] = el
	if m.ll.Len() > m.capacity {
		m.evictOldest()
	}
}

// Delete removes key if present.
func (m *lruTimeMap) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.ll.Remove(el)
		delete(m.items, key)
	}
}

// Len returns the current number of stored entries.
func (m *lruTimeMap) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ll.Len()
}

// evictOldest removes the least-recently-used entry. Caller must hold m.mu.
func (m *lruTimeMap) evictOldest() {
	el := m.ll.Back()
	if el == nil {
		return
	}
	m.ll.Remove(el)
	delete(m.items, el.Value.(*lruTimeEntry).key)
}

// shardedLatch is a fixed-size pool of channel-based latches used to serialize
// compaction operations per session. Each latch is a buffered channel of
// capacity 1 pre-filled with a single token: acquiring is <-latch (which can be
// selected against a context's Done channel, preserving cancellation-aware
// waiting) and releasing is latch <- struct{}{}. Because the pool is a fixed
// array, it is inherently bounded and never leaks; there is no eviction and
// therefore no risk of discarding a latch that is currently held or awaited.
type shardedLatch struct {
	latches []chan struct{}
}

// newShardedLatch builds a pool of n pre-filled latches. A non-positive n is
// treated as 1.
func newShardedLatch(n int) *shardedLatch {
	if n <= 0 {
		n = 1
	}
	s := &shardedLatch{latches: make([]chan struct{}, n)}
	for i := range s.latches {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		s.latches[i] = ch
	}
	return s
}

// get returns the latch responsible for the given key. The same key always maps
// to the same latch, guaranteeing same-session serialization.
func (s *shardedLatch) get(key string) chan struct{} {
	return s.latches[fnv32a(key)%uint32(len(s.latches))]
}

// fnv32a is the 32-bit FNV-1a hash, inlined to avoid an allocation per call.
func fnv32a(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}