package skillapp

import "container/list"

// invocationLRU is a minimal, fixed-capacity LRU cache for pending
// invocations. It is a hot-path accelerator only: the authoritative state
// lives in the InvocationStore, so evicting an entry never loses data — a
// subsequent lookup simply misses the cache and falls back to the store.
// O(1) get/put/evict via a doubly linked list keyed by a map.
type invocationLRU struct {
	cap   int
	ll    *list.List
	items map[string]*list.Element
}

type invocationEntry struct {
	key string
	inv *Invocation
}

func newInvocationLRU(capacity int) *invocationLRU {
	if capacity < 1 {
		capacity = 1
	}
	return &invocationLRU{cap: capacity, ll: list.New(), items: make(map[string]*list.Element, capacity)}
}

// get returns the cached invocation and marks it most-recently-used.
func (c *invocationLRU) get(key string) (*Invocation, bool) {
	if c == nil {
		return nil, false
	}
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*invocationEntry).inv, true
}

// put inserts or refreshes an entry, evicting the least-recently-used item
// once capacity is exceeded.
func (c *invocationLRU) put(key string, inv *Invocation) {
	if c == nil {
		return
	}
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*invocationEntry).inv = inv
		return
	}
	el := c.ll.PushFront(&invocationEntry{key: key, inv: inv})
	c.items[key] = el
	if c.ll.Len() > c.cap {
		c.evictOldest()
	}
}

// remove drops one entry if present.
func (c *invocationLRU) remove(key string) {
	if c == nil {
		return
	}
	if el, ok := c.items[key]; ok {
		c.ll.Remove(el)
		delete(c.items, key)
	}
}

func (c *invocationLRU) evictOldest() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*invocationEntry).key)
}