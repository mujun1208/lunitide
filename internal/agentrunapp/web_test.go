package agentrunapp

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// TestNewInstallsDefaultSearchFetcher pins the degraded-SERP fix wiring: the
// service must come up with the browser-agent search fetcher installed, so
// WebSearch's searchFetch() never silently degrades to the plain transport.
func TestNewInstallsDefaultSearchFetcher(t *testing.T) {
	s := New(nil)
	if s.searchFetchWeb == nil {
		t.Fatal("New must install the search-page fetcher")
	}
	if got := s.searchFetch(); got == nil {
		t.Fatal("searchFetch must resolve a transport")
	}
}

// TestSearchFetchPrefersTheSearchFetcher verifies the selection rule: the
// dedicated search fetcher wins when set, and the plain web fetcher serves
// otherwise (back-compat for transports injected via SetWebFetcher only).
func TestSearchFetchPrefersTheSearchFetcher(t *testing.T) {
	s := New(nil)
	plain := func(context.Context, string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{Status: 200, Body: []byte("plain")}, nil
	}
	dedicated := func(context.Context, string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{Status: 200, Body: []byte("dedicated")}, nil
	}
	// Installing the whole transport takes search pages with it.
	s.SetWebFetcher(plain)
	page, err := s.searchFetch()(context.Background(), "https://example.test/")
	if err != nil || !strings.Contains(string(page.Body), "plain") {
		t.Fatalf("fallback broken: %+v %v", page, err)
	}
	s.SetSearchWebFetcher(dedicated)
	page, err = s.searchFetch()(context.Background(), "https://example.test/")
	if err != nil || !strings.Contains(string(page.Body), "dedicated") {
		t.Fatalf("search fetcher must win: %+v %v", page, err)
	}
}
