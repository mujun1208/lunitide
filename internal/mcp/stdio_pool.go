// P1-1 stdio session pooling: persistent per-endpoint MCP stdio sessions
// replace dial-per-call spawning. Each invocation previously paid process
// spawn + initialize/tools handshake (hundreds of ms; npx-backed servers
// regularly take seconds). The pool keeps one live session per endpoint
// key, serializes calls per endpoint (the documented StdioSession
// contract: "Not safe for concurrent use: the registry serialises calls
// per endpoint"), redials after transport failures, reaps idle sessions
// and evicts the oldest idle entry when the pool is full.
//
// The pool never touches mcp6 registry lifecycle: state gating, breaker
// accounting and capability pinning stay in Registry.Invoke, which calls
// through here per authorized invocation.
package mcp

import (
	"context"
	"sync"
	"time"
)

// StdioConn is the pooled-session surface (implemented by *StdioSession).
type StdioConn interface {
	CallTool(ctx context.Context, tool string, argsJSON []byte) (StdioCallResult, error)
	Close()
}

// StdioDialFunc dials one fresh session for a pool key.
type StdioDialFunc func(ctx context.Context) (StdioConn, error)

// Pool sizing defaults: at most 8 concurrent live stdio servers; a session
// idle beyond 5 minutes is closed by the reaper so orphaned servers do not
// accumulate. The reaper scans once per minute.
const (
	StdioPoolDefaultMax   = 8
	StdioPoolDefaultIdle  = 5 * time.Minute
	StdioPoolReapInterval = time.Minute
)

type pooledConn struct {
	mu       sync.Mutex // serializes calls on this endpoint (StdioSession contract)
	conn     StdioConn  // nil after a failure; redialed on next Invoke
	lastUsed time.Time
}

type StdioPool struct {
	mu    sync.Mutex
	conns map[string]*pooledConn
	max   int
	idle  time.Duration
	now   func() time.Time
}

func NewStdioPool(max int, idle time.Duration) *StdioPool {
	if max <= 0 {
		max = StdioPoolDefaultMax
	}
	if idle <= 0 {
		idle = StdioPoolDefaultIdle
	}
	return &StdioPool{conns: make(map[string]*pooledConn), max: max, idle: idle, now: time.Now}
}

// Invoke runs one tool call on the pooled session for key, dialing on
// first use or after a failure. Concurrent Invokes on the same key queue
// on the per-endpoint mutex; a call error destroys the session (protocol
// state is uncertain after a transport failure) and the next Invoke
// redials. Dial and call errors are returned verbatim so the registry's
// breaker accounting keeps working unchanged.
func (p *StdioPool) Invoke(ctx context.Context, key string, dial StdioDialFunc, call func(StdioConn) (StdioCallResult, error)) (StdioCallResult, error) {
	p.mu.Lock()
	entry, ok := p.conns[key]
	if ok {
		// Serialize against other callers on the same endpoint before
		// releasing the pool lock; dial-under-lock keeps the first caller
		// the only dialer.
		entry.mu.Lock()
		p.mu.Unlock()
	} else {
		// Capacity: evict the oldest idle entry. Candidates are entries
		// whose mutex TryLocks (busy ones are skipped untouched); all
		// losers are unlocked again, the oldest winner is closed.
		if len(p.conns) >= p.max {
			type candidate struct {
				key      string
				entry    *pooledConn
				lastUsed time.Time
			}
			var candidates []candidate
			for k, e := range p.conns {
				if e.mu.TryLock() {
					candidates = append(candidates, candidate{key: k, entry: e, lastUsed: e.lastUsed})
				}
			}
			if len(candidates) > 0 {
				oldest := candidates[0]
				for _, c := range candidates[1:] {
					if c.lastUsed.Before(oldest.lastUsed) {
						oldest = c
					}
				}
				for _, c := range candidates {
					if c.key != oldest.key {
						c.entry.mu.Unlock()
					}
				}
				if oldest.entry.conn != nil {
					oldest.entry.conn.Close()
				}
				delete(p.conns, oldest.key)
				oldest.entry.mu.Unlock()
			}
		}
		entry = &pooledConn{}
		entry.mu.Lock()
		p.conns[key] = entry
		p.mu.Unlock()
	}
	defer entry.mu.Unlock()

	if entry.conn == nil {
		conn, err := dial(ctx)
		if err != nil {
			return StdioCallResult{}, err
		}
		entry.conn = conn
	}
	entry.lastUsed = p.now()
	out, err := call(entry.conn)
	if err != nil {
		entry.conn.Close()
		entry.conn = nil
		return out, err
	}
	entry.lastUsed = p.now()
	return out, nil
}

// Start launches the idle reaper until ctx is cancelled. Safe to call
// once per pool; subsequent calls are no-ops.
func (p *StdioPool) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(StdioPoolReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.reapIdle()
			}
		}
	}()
}

// reapIdle closes sessions idle beyond the pool idle timeout. Busy
// sessions (TryLock fails) are skipped untouched.
func (p *StdioPool) reapIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.conns {
		if entry.conn == nil {
			continue
		}
		if p.now().Sub(entry.lastUsed) < p.idle {
			continue
		}
		if !entry.mu.TryLock() {
			continue
		}
		if entry.conn != nil && p.now().Sub(entry.lastUsed) >= p.idle {
			entry.conn.Close()
			entry.conn = nil
		}
		entry.mu.Unlock()
		if entry.conn == nil {
			delete(p.conns, key)
		}
	}
}

// Close tears down every live session. In-flight calls finish first
// (their own mutexes are held); later Invokes redial.
func (p *StdioPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.conns {
		entry.mu.Lock()
		if entry.conn != nil {
			entry.conn.Close()
			entry.conn = nil
		}
		entry.mu.Unlock()
		delete(p.conns, key)
	}
}

// Len reports the number of pooled entries (diagnostics/tests).
func (p *StdioPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}
