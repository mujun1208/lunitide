package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestWeatherUsesDedicatedTransportWithoutChangingWebFetcher(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	weatherCalls, webCalls := 0, 0
	r.SetWeatherFetcher(func(_ context.Context, _ string, modified string) (networkpolicy.FetchResult, error) {
		weatherCalls++
		if modified != "" {
			t.Fatal("initial weather validator not empty")
		}
		return networkpolicy.FetchResult{}, errors.New("isolated weather callback")
	})
	r.SetWebFetcher(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		webCalls++
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/html", Body: []byte(fakePage)}, nil
	})
	if _, err := r.Execute(context.Background(), Approval, "weather-transport", "weather.get", json.RawMessage(`{"place":"合肥"}`), false); err == nil || err.Error() != "isolated weather callback" {
		t.Fatalf("weather callback not used: %v", err)
	}
	if _, err := r.Execute(context.Background(), Approval, "weather-transport", "web.fetch", json.RawMessage(`{"url":"https://example.com/"}`), false); err != nil {
		t.Fatal(err)
	}
	if weatherCalls != 1 || webCalls != 1 {
		t.Fatalf("transports crossed: weather=%d web=%d", weatherCalls, webCalls)
	}
}
