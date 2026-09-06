package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStdioPoolBusyCapacityAndSameKeyWaitHonorCancellation(t *testing.T) {
	pool := NewStdioPool(1, 0)
	defer pool.Close()
	var dials atomic.Int64
	dial := func(context.Context) (StdioConn, error) { dials.Add(1); return &fakeConn{}, nil }
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = pool.Invoke(context.Background(), "busy", dial, func(StdioConn) (StdioCallResult, error) { close(started); <-release; return StdioCallResult{}, nil })
	}()
	<-started
	defer func() { close(release); <-done }()
	for _, key := range []string{"busy", "new"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := pool.Invoke(ctx, key, dial, func(StdioConn) (StdioCallResult, error) {
			t.Error("canceled waiter executed")
			return StdioCallResult{}, nil
		})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("key %s: %v", key, err)
		}
		if pool.Len() != 1 || dials.Load() != 1 {
			t.Fatalf("capacity exceeded: len=%d dials=%d", pool.Len(), dials.Load())
		}
	}
}

func TestStdioPoolEvictionRetiresBusySessionWithoutOversubscription(t *testing.T) {
	pool := NewStdioPool(1, 0)
	conn := &fakeConn{}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = pool.Invoke(context.Background(), "old", func(context.Context) (StdioConn, error) { return conn, nil }, func(StdioConn) (StdioCallResult, error) { close(started); <-release; return StdioCallResult{}, nil })
	}()
	<-started
	pool.Evict("old")
	if pool.Len() != 1 {
		t.Fatal("busy retiring connection stopped counting toward capacity")
	}
	close(release)
	<-done
	if pool.Len() != 0 || !conn.closed.Load() {
		t.Fatal("retired session survived completion")
	}
}
