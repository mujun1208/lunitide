package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

const reliableBingPage = `<li class="b_algo"><h2><a href="https://go.dev/">Go</a></h2><p>The Go language.</p></li>`

func TestSearchHTTPFailureCannotMasqueradeAsResults(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	r.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		calls = append(calls, rawURL)
		if strings.Contains(rawURL, "duckduckgo") {
			return networkpolicy.FetchResult{Status: 429, Body: []byte(ddgLiteBody)}, nil
		}
		return networkpolicy.FetchResult{Status: 200, Body: []byte(reliableBingPage)}, nil
	})
	response, err := r.searchWeb(context.Background(), "Go", 5)
	if err != nil || response.Source != "bing" || len(calls) != 2 {
		t.Fatalf("result=%+v calls=%v err=%v", response, calls, err)
	}

	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{Status: 503, Body: []byte(ddgLiteBody + reliableBingPage)}, nil
	})
	if _, err := r.searchWeb(context.Background(), "another", 5); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("HTTP failure accepted: %v", err)
	}
}

func TestSearchCancellationStopsFallbackAndDoesNotCacheLateSuccess(t *testing.T) {
	r, _ := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		calls++
		cancel()
		return networkpolicy.FetchResult{Status: 200, Body: []byte(ddgLiteBody)}, nil
	})
	if _, err := r.searchWeb(ctx, "Go", 5); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		calls++
		return networkpolicy.FetchResult{Status: 200, Body: []byte(ddgLiteBody)}, nil
	})
	if result, err := r.searchWeb(context.Background(), "Go", 5); err != nil || result.Cached || calls != 2 {
		t.Fatalf("late success cached: %+v %d %v", result, calls, err)
	}
	if _, err := r.searchWeb(ctx, "Go", 5); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("cancelled caller received cached success: %d %v", calls, err)
	}
}

func TestSearchAttemptTimeoutAllowsHealthyFallback(t *testing.T) {
	r, _ := New(t.TempDir())
	r.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		if strings.Contains(rawURL, "duckduckgo") {
			return networkpolicy.FetchResult{}, context.DeadlineExceeded
		}
		return networkpolicy.FetchResult{Status: 200, Body: []byte(reliableBingPage)}, nil
	})
	if result, err := r.searchWeb(context.Background(), "Go", 5); err != nil || result.Source != "bing" {
		t.Fatalf("attempt timeout broke fallback: %+v %v", result, err)
	}
}

func TestSearchCachePreservesRetrievalTimeAndExpires(t *testing.T) {
	r, _ := New(t.TempDir())
	now := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	calls := 0
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		calls++
		return networkpolicy.FetchResult{Status: 200, Body: []byte(ddgLiteBody)}, nil
	})
	args := json.RawMessage(`{"query":"Go","max":3}`)
	run := func() Result {
		t.Helper()
		out, err := r.Execute(context.Background(), Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "web.search", args, false)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := run()
	if !strings.Contains(first.Output, "retrievedAt: 2026-09-07T01:00:00Z\ncached: false") {
		t.Fatal(first.Output)
	}
	now = now.Add(9 * time.Minute)
	second := run()
	if calls != 1 || !strings.Contains(second.Output, "retrievedAt: 2026-09-07T01:00:00Z\ncached: true") {
		t.Fatalf("calls=%d %s", calls, second.Output)
	}
	now = now.Add(time.Minute)
	third := run()
	if calls != 2 || !strings.Contains(third.Output, "retrievedAt: 2026-09-07T01:10:00Z\ncached: false") {
		t.Fatalf("calls=%d %s", calls, third.Output)
	}
	if _, err := r.searchWeb(context.Background(), "Go", 10); err != nil || calls != 3 {
		t.Fatalf("max bound shared incorrectly: %d %v", calls, err)
	}
	if first.Artifact == nil || second.Artifact == nil || first.Artifact.Content != second.Artifact.Content {
		t.Fatal("cache lost clickable result artifact")
	}
}

func TestSearchCacheIsBoundedAndKeepsRecentlyUsedQuery(t *testing.T) {
	r, _ := New(t.TempDir())
	calls := 0
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		calls++
		return networkpolicy.FetchResult{Status: 200, Body: []byte(ddgLiteBody)}, nil
	})
	for i := 0; i < webSearchCacheSize; i++ {
		if _, err := r.searchWeb(context.Background(), fmt.Sprintf("query %d", i), 5); err != nil {
			t.Fatal(err)
		}
	}
	if hot, err := r.searchWeb(context.Background(), "query 0", 5); err != nil || !hot.Cached {
		t.Fatalf("hot=%+v %v", hot, err)
	}
	if _, err := r.searchWeb(context.Background(), "query new", 5); err != nil {
		t.Fatal(err)
	}
	if hot, err := r.searchWeb(context.Background(), "query 0", 5); err != nil || !hot.Cached {
		t.Fatalf("recent query lost: %+v %v", hot, err)
	}
	if cold, err := r.searchWeb(context.Background(), "query 1", 5); err != nil || cold.Cached {
		t.Fatalf("oldest retained: %+v %v", cold, err)
	}
	if len(r.webSearchCache.entries) != webSearchCacheSize || calls != webSearchCacheSize+2 {
		t.Fatalf("entries=%d calls=%d", len(r.webSearchCache.entries), calls)
	}
}

func TestSearchDoesNotCacheEmptyErrorOrVerificationPages(t *testing.T) {
	for _, body := range []string{`<html>No results</html>`, `<form id="challenge-form">verify</form>` + ddgLiteBody + reliableBingPage} {
		t.Run(body[:12], func(t *testing.T) {
			r, _ := New(t.TempDir())
			calls := 0
			r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
				calls++
				return networkpolicy.FetchResult{Status: 200, Body: []byte(body)}, nil
			})
			for i := 0; i < 2; i++ {
				result, err := r.searchWeb(context.Background(), "Go", 5)
				if result.Cached || len(result.Results) != 0 {
					t.Fatalf("unverified results: %+v", result)
				}
				if strings.Contains(body, "challenge-form") && err == nil {
					t.Fatal("challenge disguised as no results")
				}
			}
			if calls != 6 {
				t.Fatalf("failure was cached: calls=%d", calls)
			}
		})
	}
	r, _ := New(t.TempDir())
	r.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		if strings.Contains(rawURL, "duckduckgo") {
			return networkpolicy.FetchResult{}, errors.New("transport unavailable")
		}
		return networkpolicy.FetchResult{Status: 200, Body: []byte("<html>Empty</html>")}, nil
	})
	if _, err := r.searchWeb(context.Background(), "Go", 5); err == nil {
		t.Fatal("partial source outage presented as definitive no results")
	}
}

func TestSearchCacheConcurrentReadersDoNotShareMutableResults(t *testing.T) {
	r, _ := New(t.TempDir())
	var calls atomic.Int32
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		calls.Add(1)
		return networkpolicy.FetchResult{Status: 200, Body: []byte(ddgLiteBody)}, nil
	})
	first, err := r.searchWeb(context.Background(), "Go", 5)
	if err != nil {
		t.Fatal(err)
	}
	first.Results[0].Title = "caller edit"
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.searchWeb(context.Background(), "Go", 5)
			if err != nil || !result.Cached || result.Results[0].Title != "Go Programming Language" {
				t.Errorf("shared result mutation: %+v %v", result, err)
				return
			}
			result.Results[0].Title = "another edit"
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("repeated network calls=%d", calls.Load())
	}
}
