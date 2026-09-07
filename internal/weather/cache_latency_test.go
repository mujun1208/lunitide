package weather

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestWeatherCacheHitDoesNotWaitForUnrelatedFetch(t *testing.T) {
	now := time.Now()
	started := make(chan struct{})
	c := New(func(ctx context.Context, _ string) (networkpolicy.FetchResult, error) {
		close(started)
		<-ctx.Done()
		return networkpolicy.FetchResult{}, ctx.Err()
	}, func() time.Time { return now })
	c.cache["cached"] = cacheEntry{fetched: now, expires: now.Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := c.load(ctx, "slow")
		done <- err
	}()
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("uncancelled fetch: %v", err)
		}
	}()
	<-started
	readCtx, stopRead := context.WithTimeout(context.Background(), time.Second)
	defer stopRead()
	if _, cached, err := c.load(readCtx, "cached"); err != nil || !cached {
		t.Fatalf("cache hit blocked behind another city: cached=%v err=%v", cached, err)
	}
}
