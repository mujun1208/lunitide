package webfetch

import "testing"

func TestSearchSnippetStaysWithItsOwnResult(t *testing.T) {
	page := `<table><tr><td><a class="result-link" href="https://example.com/first">First without snippet</a></td></tr><tr><td><a class="result-link" href="https://example.com/second">Second</a></td></tr><tr><td class="result-snippet">Only the second source says this.</td></tr></table>`
	results := ParseSearchResults(page, 5)
	if len(results) != 2 || results[0].Snippet != "" || results[1].Snippet != "Only the second source says this." {
		t.Fatalf("cross-source snippet: %+v", results)
	}
	limited := ParseSearchResults(page, 1)
	if len(limited) != 1 || limited[0].Snippet != "" {
		t.Fatalf("max=1 borrowed following evidence: %+v", limited)
	}
}
