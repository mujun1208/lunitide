package weather

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestWeatherConditionalRevalidationPreservesDataOriginTime(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	firstAt := now
	modified := now.Add(-30 * time.Minute).Format(http.TimeFormat)
	var calls int
	c := New(nil, func() time.Time { return now })
	c.FetchConditional = func(_ context.Context, raw, ims string) (networkpolicy.FetchResult, error) {
		calls++
		page := networkpolicy.FetchResult{FinalURL: raw, Expires: now.Add(time.Hour).Format(http.TimeFormat), LastModified: modified}
		if calls == 1 {
			if ims != "" {
				t.Fatalf("invented validator: %q", ims)
			}
			page.Status, page.ContentType, page.Body = 200, "application/json", sample(now)
		} else {
			if ims != modified {
				t.Fatalf("original Last-Modified not sent: %q", ims)
			}
			page.Status = 304
		}
		return page, nil
	}
	request := Request{Place: "合肥", Days: 2}
	first, err := c.Get(context.Background(), request)
	if err != nil || first.Cached || !first.CheckedAt.Equal(firstAt) {
		t.Fatalf("initial: %+v %v", first, err)
	}
	now = firstAt.Add(59 * time.Minute)
	if cached, err := c.Get(context.Background(), request); err != nil || !cached.Cached || calls != 1 {
		t.Fatalf("requested before Expires: %+v %d %v", cached, calls, err)
	}
	now = firstAt.Add(time.Hour)
	checked, err := c.Get(context.Background(), request)
	if err != nil || !checked.Cached || calls != 2 || !checked.RetrievedAt.Equal(first.RetrievedAt) || !checked.UpdatedAt.Equal(first.UpdatedAt) || !checked.CheckedAt.Equal(now) || len(checked.Days) == 0 {
		t.Fatalf("304 result: %+v calls=%d %v", checked, calls, err)
	}
	now = now.Add(time.Minute)
	if cached, err := c.Get(context.Background(), request); err != nil || !cached.Cached || calls != 2 || !cached.CheckedAt.Equal(checked.CheckedAt) {
		t.Fatalf("304 did not renew cache: %+v %d %v", cached, calls, err)
	}
}

func TestWeatherUnsolicited304CannotCreateForecast(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	c := New(nil, func() time.Time { return now })
	c.FetchConditional = func(_ context.Context, raw, _ string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 304}, nil
	}
	if _, err := c.Get(context.Background(), Request{Place: "合肥"}); err == nil || !strings.Contains(err.Error(), "没有对应") {
		t.Fatalf("unsolicited 304 accepted: %v", err)
	}
	if len(c.cache) != 0 {
		t.Fatal("invented cache")
	}
}

func TestWeatherInvalidOrFutureLastModifiedIsNotSentBack(t *testing.T) {
	for _, header := range []string{"not a date", "Mon, 07 Sep 2037 01:00:00 GMT", "Mon, 07 Sep 2026 01:00:00 GMT\r\nX: yes"} {
		t.Run(header[:10], func(t *testing.T) {
			now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
			calls := 0
			c := New(nil, func() time.Time { return now })
			c.FetchConditional = func(_ context.Context, raw, ims string) (networkpolicy.FetchResult, error) {
				calls++
				if ims != "" {
					t.Fatalf("invalid validator transmitted: %q", ims)
				}
				return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now), LastModified: header, Expires: now.Format(http.TimeFormat)}, nil
			}
			for i := 0; i < 2; i++ {
				if _, err := c.Get(context.Background(), Request{Place: "合肥"}); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatal("already expired response incorrectly cached")
			}
		})
	}
}

func TestWeather304CancellationAndChangedLocationDoNotRenewCache(t *testing.T) {
	for _, mode := range []string{"cancel", "location", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := New(nil, func() time.Time { return now })
			calls := 0
			c.FetchConditional = func(_ context.Context, raw, _ string) (networkpolicy.FetchResult, error) {
				calls++
				page := networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now), LastModified: now.Add(-time.Hour).Format(http.TimeFormat), Expires: now.Add(time.Minute).Format(http.TimeFormat)}
				if calls > 1 {
					page.Status = 304
					page.Body = nil
					switch mode {
					case "cancel":
						cancel()
					case "location":
						page.FinalURL = Endpoint + "?lat=0.0000&lon=0.0000"
					case "expiry":
						page.Expires = now.AddDate(10, 0, 0).Format(http.TimeFormat)
					}
				}
				return page, nil
			}
			first, err := c.Get(ctx, Request{Place: "合肥"})
			if err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Minute)
			if _, err = c.Get(ctx, Request{Place: "合肥"}); err == nil || mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("late/invalid 304 accepted: %v", err)
			}
			for _, entry := range c.cache {
				if !entry.checked.Equal(first.CheckedAt) {
					t.Fatal("invalid 304 renewed cache")
				}
			}
		})
	}
}
