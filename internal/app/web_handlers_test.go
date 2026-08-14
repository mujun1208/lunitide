package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// M4-G web.fetch/web.search bridge tests. The network is replaced by a canned
// WebFetcher; the tests assert the committed provenance evidence, the run
// event, idempotent replay and the SSRF error mapping.

func webCall(e *Engine, method bridge.Method, payload map[string]any, key string) bridge.Response {
	body, _ := json.Marshal(payload)
	return e.Handle(context.Background(), agentRunRequest(method, string(body), key))
}

func fetcherReturning(page networkpolicy.FetchResult, err error) agentrunapp.WebFetcher {
	return func(context.Context, string) (networkpolicy.FetchResult, error) {
		return page, err
	}
}

func runEvidence(t *testing.T, store *storage.Store, runID string) []agentrun.Evidence {
	t.Helper()
	var out []agentrun.Evidence
	err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		evidence, err := tx.ListEvidence(runID)
		out = evidence
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func runEventsOfType(t *testing.T, store *storage.Store, runID, eventType string) []agentrun.RunEvent {
	t.Helper()
	var matched []agentrun.RunEvent
	err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		events, err := tx.ListEvents(runID)
		if err != nil {
			return err
		}
		for _, ev := range events {
			if ev.EventType == eventType {
				matched = append(matched, ev)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return matched
}

type webFetchOut struct {
	Evidence struct {
		ID            string `json:"id"`
		RunID         string `json:"runId"`
		Kind          string `json:"kind"`
		SourceURI     string `json:"sourceUri"`
		ContentDigest string `json:"contentDigest"`
	} `json:"evidence"`
	FinalURL      string `json:"finalUrl"`
	Status        int    `json:"status"`
	ContentType   string `json:"contentType"`
	Title         string `json:"title"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"textTruncated"`
	FetchedBytes  int64  `json:"fetchedBytes"`
}

func decodeWebFetch(t *testing.T, res bridge.Response) webFetchOut {
	t.Helper()
	body, _ := json.Marshal(res.Payload)
	var out webFetchOut
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWebFetchLifecycleRecordsEvidence(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{
		FinalURL:    "https://example.com/doc",
		Status:      200,
		ContentType: "text/html; charset=utf-8",
		Body:        []byte(`<html><head><title>Doc &amp; Spec</title><script>evil()</script></head><body><h1>Hello</h1><p>Body text</p></body></html>`),
	}, nil))
	run := startAgentRun(t, e, sessionID, "web-life-run")

	res := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "https://example.com/doc"}, "web-life-fetch")
	if !res.OK {
		t.Fatalf("fetch: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
	out := decodeWebFetch(t, res)
	if out.Status != 200 || out.Title != "Doc & Spec" || out.FinalURL != "https://example.com/doc" {
		t.Fatalf("out=%+v", out)
	}
	if strings.Contains(out.Text, "evil") || !strings.Contains(out.Text, "Hello") || !strings.Contains(out.Text, "Body text") {
		t.Fatalf("text=%q", out.Text)
	}
	if out.FetchedBytes <= 0 {
		t.Fatalf("fetchedBytes=%d", out.FetchedBytes)
	}
	// Evidence carries the provenance triple and matches the run.
	ev := out.Evidence
	if ev.RunID != run.ID || ev.Kind != "web.fetch" || ev.SourceURI != "https://example.com/doc" || len(ev.ContentDigest) != 64 {
		t.Fatalf("evidence=%+v", ev)
	}
	// Persisted: exactly one evidence row and one EvidenceRecorded run event.
	persisted := runEvidence(t, store, run.ID)
	if len(persisted) != 1 || persisted[0].ID != ev.ID || persisted[0].ContentDigest != ev.ContentDigest {
		t.Fatalf("persisted=%+v", persisted)
	}
	events := runEventsOfType(t, store, run.ID, agentrun.EventEvidenceRecorded)
	if len(events) != 1 || !strings.Contains(string(events[0].Payload), ev.ID) {
		t.Fatalf("events=%+v", events)
	}
}

func TestWebFetchIdempotentReplayAndConflict(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	var fetches int
	e.agentRuns.SetWebFetcher(func(context.Context, string) (networkpolicy.FetchResult, error) {
		fetches++
		return networkpolicy.FetchResult{
			FinalURL:    "https://example.com/a",
			Status:      200,
			ContentType: "text/plain",
			Body:        []byte("payload"),
		}, nil
	})
	run := startAgentRun(t, e, sessionID, "web-idem-run")
	payload := map[string]any{"runId": run.ID, "url": "https://example.com/a"}

	first := webCall(e, bridge.MethodWebFetch, payload, "web-idem-fetch")
	if !first.OK {
		t.Fatalf("first: code=%s msg=%s", first.Error.Code, first.Error.Message)
	}
	replay := webCall(e, bridge.MethodWebFetch, payload, "web-idem-fetch")
	if !replay.OK {
		t.Fatalf("replay: code=%s msg=%s", replay.Error.Code, replay.Error.Message)
	}
	if decodeWebFetch(t, first).Evidence.ID != decodeWebFetch(t, replay).Evidence.ID {
		t.Fatal("replay must return the committed evidence")
	}
	if fetches != 1 {
		t.Fatalf("replay hit the network: fetches=%d", fetches)
	}
	if got := runEvidence(t, store, run.ID); len(got) != 1 {
		t.Fatalf("replay duplicated evidence: %+v", got)
	}

	conflict := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "https://example.com/other"}, "web-idem-fetch")
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict=%#v", conflict)
	}
}

func TestWebFetchValidationAndKeyRequired(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{
		FinalURL: "https://example.com/", Status: 200, ContentType: "text/plain", Body: []byte("x"),
	}, nil))
	run := startAgentRun(t, e, sessionID, "web-val-run")

	noKey := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "https://example.com/"}, "")
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("noKey=%#v", noKey)
	}
	for name, payload := range map[string]map[string]any{
		"bad run id":   {"runId": "not-a-ulid", "url": "https://example.com/"},
		"empty url":    {"runId": run.ID, "url": ""},
		"oversize url": {"runId": run.ID, "url": "https://example.com/" + strings.Repeat("a", 2048)},
	} {
		res := webCall(e, bridge.MethodWebFetch, payload, "web-val-"+name)
		if res.OK || res.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Errorf("%s=%#v", name, res)
		}
	}
}

func TestWebFetchSSRFDeniedMapping(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{},
		&networkpolicy.Error{Code: networkpolicy.CodeSSRFBlocked, Op: "resolve host"}))
	run := startAgentRun(t, e, sessionID, "web-ssrf-run")

	res := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "http://169.254.169.254/latest/meta-data"}, "web-ssrf-fetch")
	if res.OK || res.Error.Code != "WEB_SSRF_DENIED" {
		t.Fatalf("res=%#v", res)
	}
	// A blocked fetch commits nothing: no evidence, no event.
	if got := runEvidence(t, store, run.ID); len(got) != 0 {
		t.Fatalf("blocked fetch recorded evidence: %+v", got)
	}
	if got := runEventsOfType(t, store, run.ID, agentrun.EventEvidenceRecorded); len(got) != 0 {
		t.Fatalf("blocked fetch recorded event: %+v", got)
	}
}

func TestWebFetchUnsupportedContentRecordsNothing(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{
		FinalURL: "https://example.com/blob", Status: 200, ContentType: "application/octet-stream", Body: []byte{0x1, 0x2},
	}, nil))
	run := startAgentRun(t, e, sessionID, "web-bin-run")

	res := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "https://example.com/blob"}, "web-bin-fetch")
	if res.OK || res.Error.Code != "WEB_CONTENT_UNSUPPORTED" {
		t.Fatalf("res=%#v", res)
	}
	if got := runEvidence(t, store, run.ID); len(got) != 0 {
		t.Fatalf("unsupported content recorded evidence: %+v", got)
	}
}

func TestWebFetchRequiresRunningRun(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	fetches := 0
	e.agentRuns.SetWebFetcher(func(context.Context, string) (networkpolicy.FetchResult, error) {
		fetches++
		return networkpolicy.FetchResult{FinalURL: "https://example.com/", Status: 200, ContentType: "text/plain", Body: []byte("x")}, nil
	})
	run := startAgentRun(t, e, sessionID, "web-state-run")

	cancel := webCall(e, bridge.MethodAgentRunCancel, map[string]any{"runId": run.ID, "expectedVersion": run.Version}, "web-state-cancel")
	if !cancel.OK {
		t.Fatalf("cancel: %#v", cancel)
	}
	res := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": run.ID, "url": "https://example.com/"}, "web-state-fetch")
	if res.OK || res.Error.Code != "AGENT_RUN_TRANSITION_INVALID" {
		t.Fatalf("res=%#v", res)
	}
	if got := runEvidence(t, store, run.ID); len(got) != 0 {
		t.Fatalf("non-running run recorded evidence: %+v", got)
	}

	missing := webCall(e, bridge.MethodWebFetch, map[string]any{"runId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "url": "https://example.com/"}, "web-state-missing")
	if missing.OK || missing.Error.Code != "AGENT_RUN_NOT_FOUND" {
		t.Fatalf("missing=%#v", missing)
	}
	if fetches != 0 {
		t.Fatalf("invalid runs reached the network: fetches=%d", fetches)
	}
}

type webSearchOut struct {
	Evidence struct {
		ID        string `json:"id"`
		RunID     string `json:"runId"`
		Kind      string `json:"kind"`
		SourceURI string `json:"sourceUri"`
	} `json:"evidence"`
	Query   string `json:"query"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	} `json:"results"`
}

func TestWebSearchLifecycleParsesResults(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	var gotURL string
	e.agentRuns.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		gotURL = rawURL
		return networkpolicy.FetchResult{
			FinalURL:    rawURL,
			Status:      200,
			ContentType: "text/html",
			Body: []byte(`<html><body><table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F">Go</a></td></tr>
<tr><td class="result-snippet">The Go language</td></tr>
<tr><td><a class="result-link" href="javascript:void(0)">dropped</a></td></tr>
</table></body></html>`),
		}, nil
	})
	run := startAgentRun(t, e, sessionID, "web-search-run")

	res := webCall(e, bridge.MethodWebSearch, map[string]any{"runId": run.ID, "query": "golang docs", "maxResults": 5}, "web-search-1")
	if !res.OK {
		t.Fatalf("search: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
	body, _ := json.Marshal(res.Payload)
	var out webSearchOut
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Query != "golang docs" || len(out.Results) != 1 {
		t.Fatalf("out=%+v", out)
	}
	if out.Results[0].URL != "https://go.dev/" || out.Results[0].Title != "Go" || out.Results[0].Snippet != "The Go language" {
		t.Fatalf("result=%+v", out.Results[0])
	}
	// The search went to the fixed endpoint with the escaped query.
	if !strings.HasPrefix(gotURL, "https://lite.duckduckgo.com/lite/?q=") || !strings.Contains(gotURL, "golang+docs") {
		t.Fatalf("search url=%q", gotURL)
	}
	// Evidence kind and source URI point at the search endpoint, not a result.
	if out.Evidence.Kind != "web.search" || out.Evidence.RunID != run.ID || !strings.HasPrefix(out.Evidence.SourceURI, "https://lite.duckduckgo.com/") {
		t.Fatalf("evidence=%+v", out.Evidence)
	}
	if got := runEvidence(t, store, run.ID); len(got) != 1 || got[0].Kind != "web.search" {
		t.Fatalf("persisted=%+v", got)
	}

	// Replay returns the same evidence without another fetch.
	replay := webCall(e, bridge.MethodWebSearch, map[string]any{"runId": run.ID, "query": "golang docs", "maxResults": 5}, "web-search-1")
	if !replay.OK {
		t.Fatalf("replay: %#v", replay)
	}
	body, _ = json.Marshal(replay.Payload)
	var replayed webSearchOut
	if err := json.Unmarshal(body, &replayed); err != nil || replayed.Evidence.ID != out.Evidence.ID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	if got := runEvidence(t, store, run.ID); len(got) != 1 {
		t.Fatalf("replay duplicated evidence: %+v", got)
	}
}

func TestWebSearchValidation(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{
		FinalURL: "https://lite.duckduckgo.com/lite/?q=x", Status: 200, ContentType: "text/html", Body: []byte("<html></html>"),
	}, nil))
	run := startAgentRun(t, e, sessionID, "web-sval-run")

	for name, payload := range map[string]map[string]any{
		"bad run id":     {"runId": "bad", "query": "x"},
		"empty query":    {"runId": run.ID, "query": ""},
		"oversize query": {"runId": run.ID, "query": strings.Repeat("q", 257)},
		"negative max":   {"runId": run.ID, "query": "x", "maxResults": -1},
		"max above cap":  {"runId": run.ID, "query": "x", "maxResults": 11},
	} {
		res := webCall(e, bridge.MethodWebSearch, payload, "web-sval-"+name)
		if res.OK || res.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Errorf("%s=%#v", name, res)
		}
	}
}
