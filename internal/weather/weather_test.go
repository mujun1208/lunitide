package weather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func sample(now time.Time) []byte {
	point := func(at time.Time, temp float64) map[string]any {
		return map[string]any{"time": at, "data": map[string]any{"instant": map[string]any{"details": map[string]float64{"air_temperature": temp}}, "next_1_hours": map[string]any{"summary": map[string]string{"symbol_code": "partlycloudy_day"}}}}
	}
	b, _ := json.Marshal(map[string]any{"properties": map[string]any{"meta": map[string]any{"updated_at": now.Add(-time.Hour), "units": map[string]string{"air_temperature": "celsius"}}, "timeseries": []any{point(now, 20), point(now.Add(time.Hour), 23), point(now.Add(24*time.Hour), 25)}}})
	return b
}

func TestOfflineLocationsAndAmbiguity(t *testing.T) {
	for _, name := range []string{"合肥", "Hefei", "合肥市"} {
		city, err := Resolve(name, "CN", "")
		if err != nil || city.Country != "CN" || city.Timezone != "Asia/Shanghai" || city.Latitude < 31 || city.Latitude > 33 || city.Longitude < 117 || city.Longitude > 118 {
			t.Fatalf("%s: %+v %v", name, city, err)
		}
	}
	if _, err := Resolve("Cambridge", "", ""); err == nil || !strings.Contains(err.Error(), "歧义") {
		t.Fatalf("ambiguous place: %v", err)
	}
	if _, err := Resolve("不存在的城市xxxx", "", ""); err == nil {
		t.Fatal("invented location")
	}
}

func TestWeatherForecastUsesCityTimezoneAndPublicCache(t *testing.T) {
	now := time.Date(2026, 9, 7, 2, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	c := New(func(ctx context.Context, raw string) (networkpolicy.FetchResult, error) {
		calls.Add(1)
		if !strings.HasPrefix(raw, Endpoint+"?lat=31.") || !strings.Contains(raw, "&lon=117.") {
			return networkpolicy.FetchResult{}, errors.New("wrong coordinate")
		}
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now), Expires: now.Add(time.Hour).Format(http.TimeFormat)}, nil
	}, func() time.Time { return now })
	first, err := c.Get(context.Background(), Request{Place: "合肥", Days: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached || len(first.Days) != 2 || first.Days[0].Date != "2026-09-07" || first.Days[0].Minimum != 20 || first.Days[0].Maximum != 23 || !strings.HasSuffix(first.Days[0].From, "+08:00") {
		t.Fatalf("bad forecast: %+v", first)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := c.Get(context.Background(), Request{Place: "Hefei", Country: "CN", Days: 2})
			if err != nil || !got.Cached || !got.RetrievedAt.Equal(first.RetrievedAt) {
				t.Errorf("cache: %+v %v", got, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("repeated public requests: %d", calls.Load())
	}
}

func TestWeatherRejectsInvalidOrUnavailableData(t *testing.T) {
	now := time.Date(2026, 9, 7, 2, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*networkpolicy.FetchResult)
	}{
		{"rate-limit", func(p *networkpolicy.FetchResult) { p.Status = 429 }},
		{"captcha", func(p *networkpolicy.FetchResult) { p.ContentType = "text/html"; p.Body = []byte("<p>验证码</p>") }},
		{"truncated", func(p *networkpolicy.FetchResult) { p.Truncated = true }},
		{"redirect", func(p *networkpolicy.FetchResult) { p.FinalURL = "https://example.com/fake" }},
		{"wrong-units", func(p *networkpolicy.FetchResult) {
			p.Body = []byte(strings.ReplaceAll(string(p.Body), "celsius", "fahrenheit"))
		}},
		{"error-json", func(p *networkpolicy.FetchResult) { p.Body = []byte(`{"error":"invalid"}`) }},
		{"old-points", func(p *networkpolicy.FetchResult) { p.Body = sample(now.Add(-7 * 24 * time.Hour)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
				p := networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: sample(now)}
				tc.change(&p)
				return p, nil
			}, func() time.Time { return now })
			if _, err := c.Get(context.Background(), Request{Place: "合肥"}); err == nil {
				t.Fatal("bad data accepted")
			}
		})
	}
}

func TestWeatherInvalidArgumentsAndCancellationDoNotFetch(t *testing.T) {
	var calls int
	c := New(func(context.Context, string) (networkpolicy.FetchResult, error) {
		calls++
		return networkpolicy.FetchResult{}, errors.New("unexpected fetch")
	}, time.Now)
	lat, lon := 91.0, 117.0
	for _, request := range []Request{{}, {Place: "合肥", Days: 8}, {Latitude: &lat, Longitude: &lon}, {Latitude: &lon}} {
		if _, err := c.Get(context.Background(), request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, Request{Place: "合肥"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if calls != 0 {
		t.Fatalf("invalid request fetched %d", calls)
	}
}

func TestWeatherWaitingRequestCanCancel(t *testing.T) {
	c := New(nil, time.Now)
	c.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, Request{Place: "合肥"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait did not cancel: %v", err)
	}
	<-c.gate
}
