// P1-1 stdio pool coverage: session reuse across calls, per-endpoint
// serialization, failure redial, capacity eviction and idle reaping.
package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct {
	id     int
	calls  atomic.Int64
	closed atomic.Bool
	// failOnce makes the next CallTool return a transport error once.
	failNext atomic.Bool
}

var fakeConnSeq atomic.Int64

func (f *fakeConn) CallTool(ctx context.Context, tool string, argsJSON []byte) (StdioCallResult, error) {
	if f.failNext.CompareAndSwap(true, false) {
		return StdioCallResult{}, errors.New("transport glitch")
	}
	f.calls.Add(1)
	return StdioCallResult{Texts: []string{"ok"}}, nil
}
func (f *fakeConn) Close() { f.closed.Store(true) }

func TestStdioPoolReusesSessionPerKey(t *testing.T) {
	pool := NewStdioPool(0, 0)
	var dials atomic.Int64
	dial := func(ctx context.Context) (StdioConn, error) {
		dials.Add(1)
		return &fakeConn{id: int(fakeConnSeq.Add(1))}, nil
	}
	call := func(c StdioConn) (StdioCallResult, error) {
		return c.CallTool(context.Background(), "t", nil)
	}
	for i := 0; i < 3; i++ {
		if _, err := pool.Invoke(context.Background(), "ep1", dial, call); err != nil {
			t.Fatal(err)
		}
	}
	if dials.Load() != 1 {
		t.Fatalf("dials = %d, want 1 (session must be reused)", dials.Load())
	}
	// A different key dials its own session.
	if _, err := pool.Invoke(context.Background(), "ep2", dial, call); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 || pool.Len() != 2 {
		t.Fatalf("dials = %d len = %d", dials.Load(), pool.Len())
	}
}

func TestStdioPoolSerializesPerEndpoint(t *testing.T) {
	pool := NewStdioPool(0, 0)
	var inside atomic.Int64
	var maxConcurrent atomic.Int64
	conn := &fakeConn{}
	dial := func(ctx context.Context) (StdioConn, error) { return conn, nil }
	block := make(chan struct{})
	call := func(c StdioConn) (StdioCallResult, error) {
		cur := inside.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inside.Add(-1)
		select {
		case <-block:
		default:
		}
		return StdioCallResult{}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pool.Invoke(context.Background(), "ep", dial, call)
		}()
	}
	wg.Wait()
	close(block)
	if maxConcurrent.Load() != 1 {
		t.Fatalf("max concurrent calls on one endpoint = %d, want 1", maxConcurrent.Load())
	}
}

func TestStdioPoolRedialsAfterFailure(t *testing.T) {
	pool := NewStdioPool(0, 0)
	first := &fakeConn{}
	var dials atomic.Int64
	var current StdioConn = first
	dial := func(ctx context.Context) (StdioConn, error) {
		dials.Add(1)
		return current, nil
	}
	call := func(c StdioConn) (StdioCallResult, error) {
		return c.CallTool(context.Background(), "t", nil)
	}
	if _, err := pool.Invoke(context.Background(), "ep", dial, call); err != nil {
		t.Fatal(err)
	}
	// Transport failure destroys the session; the caller sees the error.
	first.failNext.Store(true)
	if _, err := pool.Invoke(context.Background(), "ep", dial, call); err == nil {
		t.Fatal("expected transport error to surface")
	}
	if !first.closed.Load() {
		t.Fatal("failed session must be closed")
	}
	// Next invoke redials transparently.
	second := &fakeConn{}
	current = second
	if _, err := pool.Invoke(context.Background(), "ep", dial, call); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Fatalf("dials = %d, want 2 (redial after failure)", dials.Load())
	}
}

func TestStdioPoolEvictsOldestIdleAtCapacity(t *testing.T) {
	pool := NewStdioPool(2, 0)
	base := time.Now()
	pool.now = func() time.Time { return base }
	dial := func(ctx context.Context) (StdioConn, error) { return &fakeConn{}, nil }
	call := func(c StdioConn) (StdioCallResult, error) { return StdioCallResult{}, nil }
	for _, key := range []string{"a", "b"} {
		if _, err := pool.Invoke(context.Background(), key, dial, call); err != nil {
			t.Fatal(err)
		}
		base = base.Add(time.Second)
	}
	// "a" is the oldest idle entry; adding "c" evicts it.
	if _, err := pool.Invoke(context.Background(), "c", dial, call); err != nil {
		t.Fatal(err)
	}
	pool.mu.Lock()
	keys := make([]string, 0, len(pool.conns))
	for k := range pool.conns {
		keys = append(keys, k)
	}
	pool.mu.Unlock()
	joined := keys[0] + keys[1]
	if joined == "ab" || joined == "ba" || pool.Len() != 2 {
		t.Fatalf("pool keys = %v, want {b,c}", keys)
	}
}

func TestStdioPoolReapsIdleSessions(t *testing.T) {
	pool := NewStdioPool(0, time.Minute)
	base := time.Now()
	pool.now = func() time.Time { return base }
	conn := &fakeConn{}
	dial := func(ctx context.Context) (StdioConn, error) { return conn, nil }
	call := func(c StdioConn) (StdioCallResult, error) { return StdioCallResult{}, nil }
	if _, err := pool.Invoke(context.Background(), "ep", dial, call); err != nil {
		t.Fatal(err)
	}
	// Not idle yet: reap is a no-op.
	pool.reapIdle()
	if pool.Len() != 1 || conn.closed.Load() {
		t.Fatal("fresh session must survive reap")
	}
	// Idle beyond the timeout: reap closes and drops it.
	pool.now = func() time.Time { return base.Add(2 * time.Minute) }
	pool.reapIdle()
	if pool.Len() != 0 || !conn.closed.Load() {
		t.Fatalf("idle session not reaped: len=%d closed=%v", pool.Len(), conn.closed.Load())
	}
}

func TestStdioPoolCloseTearsDownAll(t *testing.T) {
	pool := NewStdioPool(0, 0)
	conns := []*fakeConn{}
	dial := func(ctx context.Context) (StdioConn, error) {
		c := &fakeConn{}
		conns = append(conns, c)
		return c, nil
	}
	call := func(c StdioConn) (StdioCallResult, error) { return StdioCallResult{}, nil }
	for _, key := range []string{"a", "b"} {
		if _, err := pool.Invoke(context.Background(), key, dial, call); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()
	if pool.Len() != 0 {
		t.Fatal("pool not emptied")
	}
	for _, c := range conns {
		if !c.closed.Load() {
			t.Fatal("session not closed")
		}
	}
}
