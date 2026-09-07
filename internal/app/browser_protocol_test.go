package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/mcp6"
)

func TestBrowserInstalledSchemaControlsTargetAndPreservesText(t *testing.T) {
	for _, key := range []string{"target", "ref", "selector"} {
		t.Run(key, func(t *testing.T) {
			schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"` + key + `":{"type":"string"},"text":{"type":"string"}},"required":["` + key + `","text"]}`)
			text := "  张三\n2100040404301  "
			args, skip := playwrightArgsForSchema(browserActCall{Op: "type", Selector: "e12", Text: text}, schema)
			if skip || len(args) != 2 || args[key] != "e12" || args["text"] != text {
				t.Fatalf("%s mapping=%v skip=%v", key, args, skip)
			}
		})
	}
	args, skip := playwrightArgsForSchema(browserActCall{Op: "wait", MS: 1500}, json.RawMessage(`{"properties":{"time":{"type":"number"}}}`))
	if skip || args["time"] != 1.5 {
		t.Fatalf("milliseconds not converted: %v", args)
	}
	if _, skip := playwrightArgsForSchema(browserActCall{Op: "click", Selector: "e12"}, json.RawMessage(`{"properties":{"unrelated":{"type":"string"}},"required":["unrelated"]}`)); !skip {
		t.Fatal("incompatible schema was sent anyway")
	}
}

type browserProtocolCall struct {
	endpoint, tool string
	args           map[string]any
}

func browserProtocolEngine(t *testing.T, failClick bool) (*Engine, *[]browserProtocolCall) {
	t.Helper()
	calls := []browserProtocolCall{}
	catalogs := map[string]map[string]mcp6.ToolSchema{
		"https://metrics.example.com": {"browser_metrics": {}},
		"https://small.example.com":   {"browser_snapshot": {}, "browser_navigate": {}},
		"https://full.example.com":    {},
	}
	for _, tool := range []string{"browser_snapshot", "browser_navigate", "browser_click", "browser_type", "browser_press_key", "browser_tabs"} {
		schema := json.RawMessage(`{"type":"object","properties":{}}`)
		if tool == "browser_click" {
			schema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"target":{"type":"string"}},"required":["target"]}`)
		}
		catalogs["https://full.example.com"][tool] = mcp6.ToolSchema{InputSchema: schema}
	}
	reg := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, func(_ context.Context, ep *mcp6.Endpoint, tool string, args map[string]any, _ []byte) (map[string]any, error) {
		calls = append(calls, browserProtocolCall{ep.ID, tool, args})
		if failClick && tool == "browser_click" {
			return map[string]any{"isError": true, "text": "Element ref is stale"}, nil
		}
		return map[string]any{"isError": false, "text": "snapshot result with [ref=e12]"}, nil
	}, fakeMcpLease{})
	reg.SetDescribeFunc(func(_ context.Context, ep *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		return catalogs[ep.URL], nil
	})
	for i, url := range []string{"https://metrics.example.com", "https://small.example.com", "https://full.example.com"} {
		id := "01ARZ3NDEKTSV4RRFFQ69G5FA" + string(rune('A'+i))
		if _, err := reg.Register(context.Background(), mcp6.EndpointInput{ID: id, Transport: "https", URL: url, AuthRef: "secretref:browser-test", Pin: mcp6.BootstrapPin(url)}); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(nil, "test")
	e.mcp6Registry = reg
	return e, &calls
}

func TestBrowserRoutesWholeActionAndSnapshotToCompatibleEndpoint(t *testing.T) {
	e, calls := browserProtocolEngine(t, false)
	for i := 0; i < 2; i++ {
		out, err := e.invokeBrowserActViaPlaywright(context.Background(), browserActCall{Op: "click", Selector: "e12"})
		if err != nil || !strings.Contains(out.Output, "snapshot after click") {
			t.Fatalf("click %d=%+v %v", i, out, err)
		}
	}
	if len(*calls) != 4 {
		t.Fatalf("redundant pre-action snapshots invalidated refs: %+v", *calls)
	}
	for i, call := range *calls {
		if call.endpoint != "01ARZ3NDEKTSV4RRFFQ69G5FAC" {
			t.Fatalf("routed to unrelated/incomplete browser: %+v", call)
		}
		if i%2 == 0 && (call.tool != "browser_click" || len(call.args) != 1 || call.args["target"] != "e12") {
			t.Fatalf("actual request violated admitted schema: %+v", call)
		}
		if i%2 == 1 && call.tool != "browser_snapshot" {
			t.Fatalf("follow-up=%+v", call)
		}
	}
	// A remembered browser cannot silently become another browser when the
	// target operation is missing; it is safer to return a specific failure.
	e.browserEndpoint.Store("01ARZ3NDEKTSV4RRFFQ69G5FAB")
	if _, _, ok := e.findPlaywrightTool("click"); ok {
		t.Fatal("action switched to a different browser")
	}
}

func TestBrowserMCPErrorIsNotReportedAsSuccessfulAction(t *testing.T) {
	e, calls := browserProtocolEngine(t, true)
	_, err := e.invokeBrowserActViaPlaywright(context.Background(), browserActCall{Op: "click", Selector: "e12"})
	if err == nil || !strings.Contains(err.Error(), "Element ref is stale") {
		t.Fatalf("server failure became success: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("failure was followed by success-looking snapshot: %+v", *calls)
	}
	for _, raw := range []string{"{}", "null", `{"isError":true,"texts":["页面加载失败","其他详情"]}`} {
		if err := browserMCPResultError(raw); err == nil {
			t.Fatalf("empty/failed response accepted: %s", raw)
		}
	}
}

func TestBrowserReadinessAndDispatchRespectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	checked := make(chan struct{}, 1)
	done := make(chan bool, 1)
	go func() {
		done <- awaitBrowserReady(ctx, time.Minute, time.Second, func() bool { checked <- struct{}{}; return false })
	}()
	<-checked
	cancel()
	select {
	case ready := <-done:
		if ready {
			t.Fatal("cancelled readiness succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel waited for startup timeout")
	}
	e, calls := browserProtocolEngine(t, false)
	if _, err := e.invokeBrowserActViaPlaywright(ctx, browserActCall{Op: "click", Selector: "e12"}); err != context.Canceled {
		t.Fatalf("cancelled dispatch=%v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("cancelled operation reached MCP")
	}
}
