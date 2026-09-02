package toolruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/webfetch"
)

// truncateRunes bounds tool output on a rune boundary.
func truncateRunes(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// searchWorkspace walks one contained session workspace subtree and
// answers "path:line: text" hits for a literal or regex query. Binary
// files (NUL byte in the first 8 KiB) and oversized files are skipped;
// the walk stops as soon as max hits accumulate.
func (r *Runtime) searchWorkspace(mode Mode, session, relPath, query string, regex bool, max int, unconfined bool) ([]string, error) {
	root, err := r.path(mode, session, relPath, false, unconfined)
	if err != nil {
		return nil, err
	}
	var re *regexp.Regexp
	if regex {
		re, err = regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}
	// P2-1 fast path: literal queries of 3+ runes ride the trigram index;
	// any index-side miss or error falls back to the linear scan below.
	if re == nil && wsFTSEligible(query, false) {
		if fts, ok, ferr := r.searchFTS(root, query, max); ferr == nil && ok {
			return fts, nil
		}
	}
	return searchLinear(root, re, query, max)
}

// searchLinear is the original scan: walk the subtree, read each
// candidate file and match line by line (regex or literal substring).
func searchLinear(root string, re *regexp.Regexp, query string, max int) ([]string, error) {
	var hits []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || len(hits) >= max {
			if e != nil {
				return e
			}
			return nil
		}
		rel, e2 := filepath.Rel(root, p)
		if e2 != nil {
			return e2
		}
		if rel == "." {
			return nil
		}
		b, e2 := os.ReadFile(p)
		if e2 != nil || len(b) > maxFile {
			return nil
		}
		probe := b
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if len(hits) >= max {
				return nil
			}
			if len(line) > 400 {
				line = truncateRunes(line, 400)
			}
			matched := false
			if re != nil {
				matched = re.MatchString(line)
			} else {
				matched = strings.Contains(line, query)
			}
			if matched {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, strings.TrimRight(line, "\r")))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return []string{"no matches"}, nil
	}
	return hits, nil
}

const searchAttemptTimeout = 8 * time.Second

func (r *Runtime) searchWeb(ctx context.Context, query string, max int) ([]webfetch.SearchResult, string, string, error) {
	attempts := []struct {
		url    string
		source string
	}{
		{webfetch.SearchURL(query), "duckduckgo"},
		{webfetch.BingCNSearchURL(query), "bing"},
		{webfetch.BingSearchURL(query), "bing"},
	}
	var lastErr error
	var lastHits []webfetch.SearchResult
	var lastSrc, lastURL string
	for _, attempt := range attempts {
		c, cancel := context.WithTimeout(ctx, searchAttemptTimeout)
		page, err := r.fetchWeb(c, attempt.url)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var hits []webfetch.SearchResult
		if attempt.source == "duckduckgo" {
			hits = webfetch.ParseSearchResults(string(page.Body), max)
		} else {
			hits = webfetch.ParseBingResults(string(page.Body), max)
		}
		pageURL := attempt.url
		if page.FinalURL != "" {
			pageURL = page.FinalURL
		}
		lastHits, lastSrc, lastURL = hits, attempt.source, pageURL
		if len(hits) > 0 {
			return hits, attempt.source, pageURL, nil
		}
	}
	if lastSrc != "" {
		return lastHits, lastSrc, lastURL, nil
	}
	if lastErr != nil {
		return nil, "", "", lastErr
	}
	return nil, "none", webfetch.BingCNSearchURL(query), nil
}
