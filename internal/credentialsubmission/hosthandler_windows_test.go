//go:build windows

package credentialsubmission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

type emptyCleanupEngine struct {
	mu     sync.Mutex
	claims int
}

func (e *emptyCleanupEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	if r.Method == "internal.credential-cleanup.claim" {
		e.mu.Lock()
		e.claims++
		e.mu.Unlock()
		return bridge.Success(r.ID, []any{}), nil
	}
	return bridge.Success(r.ID, map[string]any{}), nil
}

func TestCleanupWorkerReclaimsExpiredSubmissionsDuringLongRun(t *testing.T) {
	c, store, _ := testCoordinator(t)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return now }
	input := draftInput(t, hash("worker-expiry"), []byte("short-lived"))
	input.TTL = time.Second
	if _, err := c.Submit(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	engine := &emptyCleanupEngine{}
	h := &HostHandler{Coordinator: c, Engine: engine, Secrets: store}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartCleanupWorker(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		remaining := len(c.entries)
		c.mu.Unlock()
		engine.mu.Lock()
		claims := engine.claims
		engine.mu.Unlock()
		if remaining == 0 && store.count() == 0 && claims > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic worker did not reclaim and drain: entries=%d secrets=%d claims=%d", remaining, store.count(), claims)
		}
		time.Sleep(time.Millisecond)
	}
}
