package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/egressproxy"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/webfetch"
	"github.com/oklog/ulid/v2"
)

// M4-G web fetch/search. A web call is a read-only external effect: nothing
// outside the engine is mutated, so the flow is fetch first (outside any
// transaction), then commit evidence + run event + audit + idempotency
// record in one transaction. A crash between fetch and commit persists
// nothing and the caller simply retries; a replay after commit returns the
// stored response without touching the network again (PRD M4-FR-07).

var (
	// ErrUnsupportedContent marks bodies the extractor refuses (binary,
	// images, archives). The fetch itself succeeded but produced no usable
	// text, so no evidence is recorded.
	ErrUnsupportedContent = errors.New("web content type is not supported for extraction")
)

// WebFetcher retrieves one URL under the egress policy. The default is
// networkpolicy.Fetch with the production options below; tests substitute a
// canned transport through Service.SetWebFetcher.
type WebFetcher func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)

// defaultWebFetch fetches through the SSRF-pinned transport. Plain HTTP is
// permitted for public web content; confidentiality for these read-only
// fetches comes from the IP egress policy, not from TLS.
func defaultWebFetch(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error) {
	return networkpolicy.Fetch(ctx, rawURL, networkpolicy.FetchOptions{
		Policy: networkpolicy.Policy{AllowHTTP: true},
		Proxy:  egressproxy.Resolver(),
	})
}

// defaultSearchFetch fetches search pages through the same SSRF-pinned
// transport but with the browser-style agent from webfetch.SearchUserAgent:
// keyless search frontends time out or degrade non-browser agents, so the
// search ladder needs the browser identity to get genuine results.
func defaultSearchFetch(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error) {
	return networkpolicy.Fetch(ctx, rawURL, networkpolicy.FetchOptions{
		Policy:    networkpolicy.Policy{AllowHTTP: true},
		Proxy:     egressproxy.Resolver(),
		UserAgent: webfetch.SearchUserAgent,
	})
}

// WebFetchInput is the validated web.fetch request.
type WebFetchInput struct {
	RunID string
	URL   string
}

// WebFetchResult is the committed fetch outcome with its evidence record.
type WebFetchResult struct {
	Evidence      agentrun.Evidence `json:"evidence"`
	FinalURL      string            `json:"finalUrl"`
	Status        int               `json:"status"`
	ContentType   string            `json:"contentType"`
	Title         string            `json:"title"`
	Text          string            `json:"text"`
	TextTruncated bool              `json:"textTruncated"`
	FetchedBytes  int64             `json:"fetchedBytes"`
}

// WebSearchInput is the validated web.search request.
type WebSearchInput struct {
	RunID      string
	Query      string
	MaxResults int
}

// WebSearchResult is the committed search outcome with its evidence record.
type WebSearchResult struct {
	Evidence agentrun.Evidence       `json:"evidence"`
	Query    string                  `json:"query"`
	Results  []webfetch.SearchResult `json:"results"`
}

const (
	webEvidenceKindFetch  = "web.fetch"
	webEvidenceKindSearch = "web.search"
	webSearchMaxCap       = 10
	// webSearchAttemptTimeout bounds one keyless search source fetch so a
	// slow or blocked endpoint falls through to the next mirror instead of
	// stalling the run (capability-self-bootstrap P1-1).
	webSearchAttemptTimeout = 8 * time.Second
)

// WebFetch retrieves one public URL through the SSRF policy and records the
// provenance evidence for the run.
func (s *Service) WebFetch(ctx context.Context, key, actor string, request any, in WebFetchInput) (WebFetchResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return WebFetchResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return WebFetchResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return WebFetchResult{}, err
	}
	// A replayed key returns the committed response without touching the
	// network again (PRD M4-FR-07); commitWebEvidence re-checks inside the
	// transaction to close the concurrent-claim race.
	result := WebFetchResult{}
	found, err := s.replayCommittedWebResult(ctx, "web.fetch", key, digest, &result)
	if err != nil {
		return WebFetchResult{}, err
	}
	if found {
		return result, nil
	}
	if err := s.requireRunningRun(ctx, in.RunID, "web.fetch"); err != nil {
		return WebFetchResult{}, err
	}
	page, err := s.fetchWeb(ctx, in.URL)
	if err != nil {
		return WebFetchResult{}, err
	}
	extracted, ok := webfetch.ExtractText(page.ContentType, page.Body, webfetch.MaxTextBytes)
	if !ok {
		return WebFetchResult{}, fmt.Errorf("%w: %s", ErrUnsupportedContent, page.ContentType)
	}
	bodyDigest := sha256.Sum256(page.Body)
	now := s.clock.Now().UTC()
	result = WebFetchResult{
		Evidence: agentrun.Evidence{
			ID:            ulid.Make().String(),
			RunID:         in.RunID,
			Kind:          webEvidenceKindFetch,
			SourceURI:     page.FinalURL,
			ContentDigest: hex.EncodeToString(bodyDigest[:]),
			CapturedAt:    now,
			CreatedAt:     now,
		},
		FinalURL:      page.FinalURL,
		Status:        page.Status,
		ContentType:   page.ContentType,
		Title:         extracted.Title,
		Text:          extracted.Text,
		TextTruncated: extracted.Truncated || page.Truncated,
		FetchedBytes:  int64(len(page.Body)),
	}
	err = s.commitWebEvidence(ctx, "web.fetch", "web.fetched", key, actor, digest, in.RunID, &result)
	return result, err
}

// WebSearch runs one query through the keyless HTML search ladder
// (DuckDuckGo Lite, then the Bing mirrors) on the same SSRF-pinned
// transport and records the result-set evidence from the winning source.
func (s *Service) WebSearch(ctx context.Context, key, actor string, request any, in WebSearchInput) (WebSearchResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return WebSearchResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return WebSearchResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return WebSearchResult{}, err
	}
	result := WebSearchResult{}
	found, err := s.replayCommittedWebResult(ctx, "web.search", key, digest, &result)
	if err != nil {
		return WebSearchResult{}, err
	}
	if found {
		return result, nil
	}
	if err := s.requireRunningRun(ctx, in.RunID, "web.search"); err != nil {
		return WebSearchResult{}, err
	}
	max := in.MaxResults
	if max <= 0 {
		max = 5
	}
	if max > webSearchMaxCap {
		max = webSearchMaxCap
	}
	// Keyless HTML search over the shared fallback ladder: one slow or
	// blocked source degrades to the next instead of failing the whole
	// search, and challenge pages and non-2xx responses are treated as
	// source failures (capability-self-bootstrap P1-1).
	var (
		results   []webfetch.SearchResult
		body      []byte
		searchURL string
		lastErr   error
	)
	for _, attempt := range webfetch.SearchAttempts(in.Query) {
		if err := ctx.Err(); err != nil {
			return WebSearchResult{}, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, webSearchAttemptTimeout)
		page, err := s.searchFetch()(attemptCtx, attempt.URL)
		cancel()
		if err := ctx.Err(); err != nil {
			return WebSearchResult{}, err
		}
		if err != nil {
			lastErr = err
			continue
		}
		if page.Status < 200 || page.Status >= 300 {
			lastErr = fmt.Errorf("web search %s returned HTTP %d; no search results confirmed", attempt.Source, page.Status)
			continue
		}
		if webfetch.ChallengePage(string(page.Body)) {
			lastErr = fmt.Errorf("web search %s requires browser verification; no search results confirmed", attempt.Source)
			continue
		}
		if _, ok := webfetch.ExtractText(page.ContentType, page.Body, 1); !ok {
			lastErr = fmt.Errorf("%w: %s", ErrUnsupportedContent, page.ContentType)
			continue
		}
		hits := webfetch.ParseSearchSource(attempt.Source, string(page.Body), max)
		if len(hits) == 0 {
			lastErr = fmt.Errorf("web search %s returned no results", attempt.Source)
			continue
		}
		results = hits
		body = page.Body
		searchURL = page.FinalURL
		if searchURL == "" {
			searchURL = attempt.URL
		}
		break
	}
	if len(results) == 0 {
		if lastErr == nil {
			lastErr = errors.New("web search returned no results from any source")
		}
		return WebSearchResult{}, lastErr
	}
	bodyDigest := sha256.Sum256(body)
	now := s.clock.Now().UTC()
	result = WebSearchResult{
		Evidence: agentrun.Evidence{
			ID:            ulid.Make().String(),
			RunID:         in.RunID,
			Kind:          webEvidenceKindSearch,
			SourceURI:     searchURL,
			ContentDigest: hex.EncodeToString(bodyDigest[:]),
			CapturedAt:    now,
			CreatedAt:     now,
		},
		Query:   in.Query,
		Results: results,
	}
	err = s.commitWebEvidence(ctx, "web.search", "web.searched", key, actor, digest, in.RunID, &result)
	return result, err
}

// replayCommittedWebResult short-circuits a replayed key before any network
// access: the committed response is returned exactly as stored. The commit
// path re-checks the claim inside its transaction, so a concurrent first use
// of the key still collapses to one committed outcome.
func (s *Service) replayCommittedWebResult(ctx context.Context, op, key, digest string, result any) (bool, error) {
	found := false
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		record, ok, err := tx.Idempotency(op, key, s.clock.Now().UTC())
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		found = true
		return replay(record, digest, result)
	})
	return found, err
}

// requireRunningRun prevents unauthorized or terminal runs from producing a
// network effect. The commit path re-checks the state to catch transitions
// that race with the request.
func (s *Service) requireRunningRun(ctx context.Context, runID, op string) error {
	return s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		run, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: %s requires a running run, got %s", agentrun.ErrInvalidTransition, op, run.Status)
		}
		return nil
	})
}

// commitWebEvidence persists one web outcome atomically: idempotency claim,
// running-run guard, append-only evidence, EvidenceRecorded run event, audit
// record. Replaying a claimed key returns the stored response without
// appending duplicate evidence.
func (s *Service) commitWebEvidence(ctx context.Context, op, auditAction, key, actor, digest string, runID string, result any) error {
	return s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(op, key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, result)
		}
		run, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: %s requires a running run, got %s", agentrun.ErrInvalidTransition, op, run.Status)
		}
		var evidence agentrun.Evidence
		switch r := result.(type) {
		case *WebFetchResult:
			evidence = r.Evidence
		case *WebSearchResult:
			evidence = r.Evidence
		default:
			return fmt.Errorf("%w: unknown web result type", agentrun.ErrInvalid)
		}
		if err := evidence.Validate(); err != nil {
			return err
		}
		if err := tx.AppendEvidence(evidence); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventEvidenceRecorded, map[string]any{
			"schemaVersion": 1,
			"runId":         run.ID,
			"evidenceId":    evidence.ID,
			"kind":          evidence.Kind,
			"sourceUri":     evidence.SourceURI,
			"contentDigest": evidence.ContentDigest,
			"capturedAt":    evidence.CapturedAt,
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"evidenceId": evidence.ID, "kind": evidence.Kind})
		if err := s.putAudit(tx, auditAction, run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: op, Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
}

// SetWebFetcher substitutes the whole fetch transport — search pages
// included: a caller that installs its own transport owns every fetch, so a
// previously installed search-page fetcher is dropped and searches ride this
// one (tests inject one mock and get every fetch from it).
func (s *Service) SetWebFetcher(f WebFetcher) {
	s.fetchWeb = f
	s.searchFetchWeb = nil
}

// searchFetch resolves the transport for search-page fetches: the dedicated
// search fetcher when set, otherwise the plain web fetcher (tests that only
// inject SetWebFetcher keep serving search pages from the same transport).
func (s *Service) searchFetch() WebFetcher {
	if s.searchFetchWeb != nil {
		return s.searchFetchWeb
	}
	return s.fetchWeb
}

// SetSearchWebFetcher substitutes the search-page fetch transport (the
// browser-agent variant). Tests use it to verify the search ladder rides it.
func (s *Service) SetSearchWebFetcher(f WebFetcher) {
	s.searchFetchWeb = f
}
