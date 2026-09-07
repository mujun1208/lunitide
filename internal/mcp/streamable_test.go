package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type remoteFixture struct {
	calls, closed, lists atomic.Int32
	mode                 string
}

func (f *remoteFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer fixture-secret" {
		t.Error("missing origin-bound credential")
		w.WriteHeader(401)
		return
	}
	if r.Method == http.MethodDelete {
		f.closed.Add(1)
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost {
		t.Errorf("unexpected method %s", r.Method)
		w.WriteHeader(405)
		return
	}
	if r.Header.Get("Accept") != "application/json, text/event-stream" {
		t.Error("missing supported response types")
	}
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		t.Error("invalid request")
		return
	}
	if req.Method != "initialize" && (r.Header.Get("Mcp-Session-Id") != "session-fixture" || r.Header.Get("MCP-Protocol-Version") != "2025-06-18") {
		t.Error("lost session/version headers")
	}
	w.Header().Set("Content-Type", "application/json")
	var result any
	switch req.Method {
	case "initialize":
		if f.mode == "unauthorized" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Mcp-Session-Id", "session-fixture")
		result = map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "fixture", "version": "1.0.0"}, "capabilities": map[string]any{"tools": map[string]any{}}}
	case "notifications/initialized":
		w.WriteHeader(202)
		return
	case "tools/list":
		f.lists.Add(1)
		if f.mode == "cursor-loop" {
			result = map[string]any{"tools": []any{}, "nextCursor": "same"}
			break
		}
		if req.Params["cursor"] == nil {
			result = map[string]any{"tools": []any{map[string]any{"name": "weather", "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": "page-2"}
		} else {
			result = map[string]any{"tools": []any{map[string]any{"name": "rail", "inputSchema": map[string]any{"type": "object"}}}}
		}
	case "tools/call":
		f.calls.Add(1)
		switch f.mode {
		case "lost-reply":
			w.WriteHeader(503)
			return
		case "expired":
			w.WriteHeader(404)
			return
		case "cancelled":
			<-r.Context().Done()
			return
		case "redirect":
			http.Redirect(w, r, "https://another-origin.invalid", http.StatusTemporaryRedirect)
			return
		case "wrong-id":
			req.ID++
		case "oversize":
			fmt.Fprint(w, strings.Repeat("x", MaxResponseBytes+1))
			return
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "合肥多云"}}, "structuredContent": map[string]any{"city": "合肥", "temperature": 26}, "isError": f.mode == "tool-error"}
	default:
		t.Errorf("unexpected RPC %s", req.Method)
		w.WriteHeader(400)
		return
	}
	answer, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	if f.mode == "sse" && req.Method == "tools/call" {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, ": keepalive\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", answer)
	} else {
		w.Write(answer)
	}
}

func TestStreamableHTTPUsesRealTLSJSONAndSSE(t *testing.T) {
	for _, mode := range []string{"json", "sse", "tool-error"} {
		t.Run(mode, func(t *testing.T) {
			f := &remoteFixture{mode: mode}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
			defer srv.Close()
			client := tlsTestClient(t, srv)
			client.BaseURL += "/mcp"
			remote, tools, err := client.Discover(context.Background(), []byte("fixture-secret"))
			if err != nil {
				t.Fatal(err)
			}
			if len(tools) != 2 || tools[1].Name != "rail" || f.lists.Load() != 2 {
				t.Fatalf("pagination: %+v", tools)
			}
			if !strings.Contains(remote.Identity(), "fixture") || strings.Contains(remote.Identity(), "fixture-secret") || remote.IsLegacy() {
				t.Fatal("invalid observed identity")
			}
			out, err := remote.Call(context.Background(), "weather", []byte(`{"city":"合肥"}`))
			if err != nil || out["text"] != "合肥多云" || out["isError"] != (mode == "tool-error") {
				t.Fatalf("call result: %+v %v", out, err)
			}
			if len(out["structured"].(json.RawMessage)) == 0 {
				t.Fatal("structured result lost")
			}
			remote.Close()
			if f.calls.Load() != 1 || f.closed.Load() != 1 || remote.bearer != nil {
				t.Fatal("lease/session not released")
			}
		})
	}
}

func TestStreamableFailuresNeverReplayToolCalls(t *testing.T) {
	for _, mode := range []string{"lost-reply", "expired", "cancelled", "wrong-id", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := &remoteFixture{mode: mode}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
			defer srv.Close()
			client := tlsTestClient(t, srv)
			client.BaseURL += "/mcp"
			remote, _, err := client.Discover(context.Background(), []byte("fixture-secret"))
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, err = remote.Call(ctx, "weather", []byte(`{}`))
			if err == nil {
				t.Fatal("invalid/lost response reported success")
			}
			if mode == "expired" && !errors.Is(err, ErrRemoteSessionExpired) {
				t.Fatal(err)
			}
			if f.calls.Load() != 1 {
				t.Fatalf("mutation replayed %d times", f.calls.Load())
			}
		})
	}
}

func TestStreamableRejectsUnauthorizedAndLoopingCatalogue(t *testing.T) {
	for _, mode := range []string{"unauthorized", "cursor-loop"} {
		t.Run(mode, func(t *testing.T) {
			f := &remoteFixture{mode: mode}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
			defer srv.Close()
			client := tlsTestClient(t, srv)
			client.BaseURL += "/mcp"
			if _, _, err := client.Discover(context.Background(), []byte("fixture-secret")); err == nil {
				t.Fatal("invalid catalogue admitted")
			}
			if f.calls.Load() != 0 || f.lists.Load() > 2 {
				t.Fatal("unbounded or unauthorized call")
			}
		})
	}
}

func TestStreamableQueryCredentialIsLeasedAndNeverLeaked(t *testing.T) {
	var requests atomic.Int32
	fixture := &remoteFixture{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("token") != "fixture-secret" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect query credential transport")
		}
		r.Header.Set("Authorization", "Bearer fixture-secret")
		fixture.serve(t, w, r)
	}))
	defer srv.Close()
	client := tlsTestClient(t, srv)
	client.BaseURL += "/mcp?token={{credential}}"
	session, _, err := client.Discover(context.Background(), []byte("fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Call(context.Background(), "weather", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(session.Identity(), "fixture-secret") || strings.Contains(client.BaseURL, "fixture-secret") {
		t.Fatal("credential escaped request lease")
	}
	session.Close()
	before := requests.Load()
	if _, _, err = client.Discover(context.Background(), nil); err == nil || requests.Load() != before {
		t.Fatal("missing token reached network")
	}
	srv.Close()
	if _, _, err = client.Discover(context.Background(), []byte("fixture-secret")); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("transport failure must redact token: %v", err)
	}
}

func TestStreamableQueryCredentialDoesNotFallbackToLegacy(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(404) }))
	defer srv.Close()
	client := tlsTestClient(t, srv)
	client.BaseURL += "/mcp?token={{credential}}"
	if _, _, err := client.Discover(context.Background(), []byte("fixture-secret")); err == nil || requests.Load() != 1 {
		t.Fatal("query-auth endpoint attempted legacy protocol")
	}
}

func TestStreamableURLRejectsPersistedSecrets(t *testing.T) {
	for _, raw := range []string{"https://example.test/mcp?token=plaintext", "https://user:password@example.test/mcp", "https://example.test/mcp?token={{credential}}&key={{credential}}", "https://example.test/mcp?token={{credential}}&token={{credential}}", "https://example.test/mcp?arbitrary={{credential}}", "https://example.test/mcp#fragment"} {
		if err := ValidateBaseURL(raw); err == nil || strings.Contains(err.Error(), "plaintext") {
			t.Fatalf("unsafe URL admitted or echoed: %v", err)
		}
	}
}
