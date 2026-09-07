package toolruntime

import (
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/webfetch"
)

const (
	webSearchCacheTTL  = 10 * time.Minute
	webSearchCacheSize = 64
	webSearchMaxHits   = 10
)

type webSearchResponse struct {
	Results     []webfetch.SearchResult
	Source      string
	PageURL     string
	RetrievedAt time.Time
	Cached      bool
}

type webSearchCacheKey struct {
	query string
	max   int
}
type webSearchCacheEntry struct {
	response webSearchResponse
	used     uint64
}

// Public web evidence is kept only in this runtime's memory, with bounded
// entry count and field lengths. Failure and empty responses are not cached.
type webSearchCache struct {
	mu      sync.Mutex
	entries map[webSearchCacheKey]webSearchCacheEntry
	clock   uint64
}

func (c *webSearchCache) get(key webSearchCacheKey, now time.Time) (webSearchResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return webSearchResponse{}, false
	}
	age := now.Sub(entry.response.RetrievedAt)
	if age < 0 || age >= webSearchCacheTTL {
		delete(c.entries, key)
		return webSearchResponse{}, false
	}
	c.clock++
	entry.used = c.clock
	c.entries[key] = entry
	response := entry.response
	response.Results = append([]webfetch.SearchResult(nil), response.Results...)
	response.Cached = true
	return response, true
}

func (c *webSearchCache) put(key webSearchCacheKey, response webSearchResponse, now time.Time) {
	if len(response.Results) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[webSearchCacheKey]webSearchCacheEntry)
	}
	for entryKey, entry := range c.entries {
		age := now.Sub(entry.response.RetrievedAt)
		if age < 0 || age >= webSearchCacheTTL {
			delete(c.entries, entryKey)
		}
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= webSearchCacheSize {
		var oldestKey webSearchCacheKey
		oldest := ^uint64(0)
		for entryKey, entry := range c.entries {
			if entry.used < oldest {
				oldest, oldestKey = entry.used, entryKey
			}
		}
		delete(c.entries, oldestKey)
	}
	c.clock++
	response.Results = append([]webfetch.SearchResult(nil), response.Results...)
	response.Cached = false
	c.entries[key] = webSearchCacheEntry{response: response, used: c.clock}
}

func boundedWebSearchResponse(hits []webfetch.SearchResult, source, pageURL string, now time.Time) webSearchResponse {
	response := webSearchResponse{Source: source, PageURL: pageURL, RetrievedAt: now.UTC()}
	for _, hit := range hits {
		// Do not clip a destination URL into a different, invalid location.
		if len(hit.URL) > 4096 {
			continue
		}
		hit.Title = truncateRunes(hit.Title, 300)
		hit.Snippet = truncateRunes(hit.Snippet, 1200)
		response.Results = append(response.Results, hit)
		if len(response.Results) == webSearchMaxHits {
			break
		}
	}
	return response
}

func searchChallengePage(body string) bool {
	// Match challenge markup, not incidental result text mentioning captcha.
	lower := strings.ToLower(body)
	for _, marker := range []string{`id="challenge-form"`, `id='challenge-form'`, `id="anomaly-modal"`, `class="anomaly-modal`, `id="b_captcha"`, `id='b_captcha'`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
