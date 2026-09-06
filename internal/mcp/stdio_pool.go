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
	retired  bool // guarded by pool.mu
}

type StdioPool struct {
	mu      sync.Mutex
	conns   map[string]*pooledConn
	max     int
	idle    time.Duration
	now     func() time.Time
	changed chan struct{}
	start   sync.Once
}

func NewStdioPool(max int, idle time.Duration) *StdioPool {
	if max <= 0 {
		max = StdioPoolDefaultMax
	}
	if idle <= 0 {
		idle = StdioPoolDefaultIdle
	}
	return &StdioPool{conns: make(map[string]*pooledConn), max: max, idle: idle, now: time.Now, changed: make(chan struct{})}
}

// Invoke runs one tool call on the pooled session for key, dialing on
// first use or after a failure. Concurrent Invokes on the same key queue
// on the per-endpoint mutex; a call error destroys the session (protocol
// state is uncertain after a transport failure) and the next Invoke
// redials. Dial and call errors are returned verbatim so the registry's
// breaker accounting keeps working unchanged.
func (p *StdioPool) Invoke(ctx context.Context, key string, dial StdioDialFunc, call func(StdioConn) (StdioCallResult, error)) (StdioCallResult, error) {
	var entry *pooledConn
	for {
		if err := ctx.Err(); err != nil {
			return StdioCallResult{}, err
		}
		p.mu.Lock()
		entry = p.conns[key]
		if entry != nil && !entry.retired && entry.mu.TryLock() {
			p.mu.Unlock()
			break
		}
		if entry == nil {
			if len(p.conns) >= p.max {
				var oldest *pooledConn
				var oldestKey string
				for k, candidate := range p.conns {
					if candidate.mu.TryLock() {
						if oldest == nil || candidate.lastUsed.Before(oldest.lastUsed) {
							if oldest != nil {
								oldest.mu.Unlock()
							}
							oldest, oldestKey = candidate, k
						} else {
							candidate.mu.Unlock()
						}
					}
				}
				if oldest != nil {
					if oldest.conn != nil {
						oldest.conn.Close()
					}
					delete(p.conns, oldestKey)
					oldest.mu.Unlock()
				}
			}
			if len(p.conns) < p.max {
				entry = &pooledConn{}
				entry.mu.Lock()
				p.conns[key] = entry
				p.mu.Unlock()
				break
			}
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return StdioCallResult{}, ctx.Err()
		case <-changed:
		}
	}
	defer func() {
		p.mu.Lock()
		if entry.retired {
			if entry.conn != nil {
				entry.conn.Close()
				entry.conn = nil
			}
			delete(p.conns, key)
		}
		entry.mu.Unlock()
		p.signalLocked()
		p.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return StdioCallResult{}, err
	}

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
	p.start.Do(func() {
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
	})
}

// reapIdle closes sessions idle beyond the pool idle timeout. Busy
// sessions (TryLock fails) are skipped untouched.
func (p *StdioPool) reapIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.conns {
		if !entry.mu.TryLock() {
			continue
		}
		if entry.conn == nil || p.now().Sub(entry.lastUsed) >= p.idle {
			if entry.conn != nil {
				entry.conn.Close()
				entry.conn = nil
			}
			delete(p.conns, key)
			p.signalLocked()
		}
		entry.mu.Unlock()
	}
}

// Close retires every session. Idle sessions close immediately; in-flight
// sessions close on completion and continue occupying capacity until then.
func (p *StdioPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.conns {
		entry.retired = true
		if entry.mu.TryLock() {
			if entry.conn != nil {
				entry.conn.Close()
				entry.conn = nil
			}
			delete(p.conns, key)
			entry.mu.Unlock()
		}
	}
	p.signalLocked()
}

// Len reports the number of pooled entries (diagnostics/tests).
func (p *StdioPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// Evict invalidates an endpoint session immediately. Busy sessions close when
// their canceled call returns, and continue occupying capacity until then.
func (p *StdioPool) Evict(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.conns[key]; entry != nil {
		entry.retired = true
		if entry.mu.TryLock() {
			if entry.conn != nil {
				entry.conn.Close()
				entry.conn = nil
			}
			delete(p.conns, key)
			entry.mu.Unlock()
		}
	}
	p.signalLocked()
}

func (p *StdioPool) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}
