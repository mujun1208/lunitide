package weather

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestForecastAtLocalMidnightDoesNotIncludeYesterday(t *testing.T) {
	// Hefei 00:30 on September 7: the one-hour tolerance must not add
	// September 6 to a request for today's single forecast day.
	now := time.Date(2026, 9, 6, 16, 30, 0, 0, time.UTC)
	c := New(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now.Add(-time.Hour))}, nil
	}, func() time.Time { return now })
	got, err := c.Get(context.Background(), Request{Place: "合肥", Days: 1})
	if err != nil || len(got.Days) != 1 || got.Days[0].Date != "2026-09-07" || got.Days[0].From != "2026-09-07T00:30:00+08:00" {
		t.Fatalf("included previous local day: %+v %v", got, err)
	}
}

func TestForecastRetrievalTimeIsAfterNetworkCompletion(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	started := now
	c := New(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		now = now.Add(2 * time.Second)
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now)}, nil
	}, func() time.Time { return now })
	got, err := c.Get(context.Background(), Request{Place: "合肥"})
	if err != nil || !got.RetrievedAt.Equal(started.Add(2*time.Second)) {
		t.Fatalf("request-start timestamp: %+v %v", got, err)
	}
}

func TestForecastCacheHonorsExpiryAndClockRollback(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	var calls int
	c := New(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		calls++
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now), Expires: now.Add(2 * time.Hour).Format(http.TimeFormat)}, nil
	}, func() time.Time { return now })
	request := Request{Place: "合肥"}
	first, err := c.Get(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	got, err := c.Get(context.Background(), request)
	if err != nil || !got.Cached || calls != 1 || !got.RetrievedAt.Equal(first.RetrievedAt) {
		t.Fatalf("did not honor upstream expiry: %+v %d %v", got, calls, err)
	}
	now = now.Add(-2 * time.Hour)
	got, err = c.Get(context.Background(), request)
	if err != nil || got.Cached || calls != 2 {
		t.Fatalf("served response retrieved in the future: %+v %d %v", got, calls, err)
	}
}

func TestForecastRejectsImplausibleExpiryWithoutKeepingIt(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	c := New(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now), Expires: now.AddDate(10, 0, 0).Format(http.TimeFormat)}, nil
	}, func() time.Time { return now })
	if _, err := c.Get(context.Background(), Request{Place: "合肥"}); err == nil || !strings.Contains(err.Error(), "有效期") {
		t.Fatalf("distant expiry accepted: %v", err)
	}
	if len(c.cache) != 0 {
		t.Fatal("implausible expiry was cached")
	}
}

func TestForecastRejectsRedirectToOtherCoordinates(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	for _, query := range []string{"lat=0.0000&lon=0.0000", "lat=31.8669&lon=117.2822&lat=0.0000", "lat=31.8669&lon=117.2822&ignored=%xx"} {
		t.Run(query, func(t *testing.T) {
			c := New(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
				return networkpolicy.FetchResult{FinalURL: Endpoint + "?" + query, Status: 200, ContentType: "application/json", Body: sample(now)}, nil
			}, func() time.Time { return now })
			if _, err := c.Get(context.Background(), Request{Place: "合肥"}); err == nil || !strings.Contains(err.Error(), "位置") {
				t.Fatalf("redirect accepted: %v", err)
			}
		})
	}
}
