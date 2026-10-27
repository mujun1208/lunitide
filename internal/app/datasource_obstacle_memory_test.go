package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/memory"
)

// capability-self-bootstrap P5: datasource obstacle memory. A walled
// web.fetch queues its host; a later successful web.search (the alternate
// path) settles the obstacle into project semantic memory — upsert, never
// duplicate — so the next similar query recalls the proven route.

func TestObserveDatasourceObstacleFlushesOnSearchSuccess(t *testing.T) {
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	state := &streamState{}

	// Direct fetch bounces off the login wall (HTTP 403).
	e.observeDatasourceObstacle(context.Background(), sessionID, "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/firm/xxx.html"}`),
		"ok:false\nurl: https://aiqicha.baidu.com/firm/xxx.html\nHTTP 403", state)
	if len(state.pendingObstacleHosts) != 1 || !state.pendingObstacleHosts["aiqicha.baidu.com"] {
		t.Fatalf("wall not queued: %v", state.pendingObstacleHosts)
	}
	if len(store.items) != 0 {
		t.Fatalf("memory written before the alternate path succeeded: %#v", store.items)
	}

	// The engine reroutes through web.search and that path works.
	e.observeDatasourceObstacle(context.Background(), sessionID, "web.search", nil,
		"query: 公司信息\nretrievedAt: 2026-10-27T00:00:00Z\n1. 公司 - 百度百科", state)
	if len(state.pendingObstacleHosts) != 0 {
		t.Fatalf("queue not flushed: %v", state.pendingObstacleHosts)
	}
	if len(store.items) != 1 {
		t.Fatalf("want 1 obstacle memory, got %#v", store.items)
	}
	m := store.items[0]
	if m.Layer != memory.LayerSemantic || m.Scope != memory.ScopeProject {
		t.Fatalf("layer/scope = %v/%v", m.Layer, m.Scope)
	}
	if m.Key != "datasource-obstacle:aiqicha.baidu.com" {
		t.Fatalf("key = %q", m.Key)
	}
	if !strings.Contains(m.Content, "登录墙") || !strings.Contains(m.Content, "web.search") {
		t.Fatalf("content = %q", m.Content)
	}

	// Re-observing the same wall upserts, never duplicates.
	e.observeDatasourceObstacle(context.Background(), sessionID, "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/firm/yyy.html"}`),
		"ok:false\nurl: https://aiqicha.baidu.com/firm/yyy.html\n403 Forbidden", state)
	e.observeDatasourceObstacle(context.Background(), sessionID, "web.search", nil,
		"query: 公司\n1. 结果", state)
	if len(store.items) != 1 {
		t.Fatalf("upsert duplicated: %#v", store.items)
	}
}

// TestObserveDatasourceObstacleDetectsLoginWallLanding covers the softer
// wall: the fetch "succeeds" but lands on an auth interstitial.
func TestObserveDatasourceObstacleDetectsLoginWallLanding(t *testing.T) {
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	state := &streamState{}

	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/firm/xxx.html"}`),
		"title: 登录_爱企查\nurl: https://passport.baidu.com/?redirect=x\n\n请登录后查看完整信息", state)
	if len(state.pendingObstacleHosts) != 1 || !state.pendingObstacleHosts["aiqicha.baidu.com"] {
		t.Fatalf("auth landing not queued: %v", state.pendingObstacleHosts)
	}

	// A plain content page must NOT look like a wall, even when its text
	// mentions the word login.
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://www.example.com/article"}`),
		"title: 公司年报解读\nurl: https://www.example.com/article\n\n本文介绍如何登录企业信用系统查询年报，内容较长，包含大量正文文本，远超登录墙页面的长度阈值，因此不应被误判为登录墙。公司年报、经营范围与股权结构详见正文。", state)
	if state.pendingObstacleHosts["www.example.com"] {
		t.Fatal("content page misread as login wall")
	}
	// The stale queue entry for its own host clears on a plain success.
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/firm/xxx.html"}`),
		"title: 正常页面\nurl: https://aiqicha.baidu.com/firm/xxx.html\n\n完整的企业信息正文内容在这里，包含工商注册信息、股东结构与历史变更记录，正文长度远超阈值。", state)
	if state.pendingObstacleHosts["aiqicha.baidu.com"] {
		t.Fatal("transient obstacle did not clear on same-host success")
	}
}

// TestObserveDatasourceObstacleHoldsWithoutAlternatePath: without a
// successful web.search in the turn, the obstacle stays queued (never
// written), because "prefer web.search" is only useful advice once that
// path actually worked.
func TestObserveDatasourceObstacleHoldsWithoutAlternatePath(t *testing.T) {
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: "01ARZ3NDEKTSV4RRFFQ69G5FAY"}
	e.memories = store
	state := &streamState{}
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://qcc.com/firm"}`),
		"ok:false\nurl: https://qcc.com/firm\nHTTP 403", state)
	e.observeDatasourceObstacle(context.Background(), "s", "web.search", nil,
		"ok:false\nweb search duckduckgo context deadline exceeded", state)
	if len(store.items) != 0 {
		t.Fatalf("wrote memory without a working alternate path: %#v", store.items)
	}
}

// TestObserveDatasourceObstacleFailsClosed: nil services, unmapped
// sessions, gate refusals — all silent no-ops, never panics.
func TestObserveDatasourceObstacleFailsClosed(t *testing.T) {
	e := NewEngine(nil, "test")
	state := &streamState{}
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/x"}`),
		"ok:false\nurl: https://aiqicha.baidu.com/x\nHTTP 403", state)
	if len(state.pendingObstacleHosts) != 1 || !state.pendingObstacleHosts["aiqicha.baidu.com"] {
		t.Fatalf("queue = %v", state.pendingObstacleHosts)
	}
	// No memory service: flush must stay silent.
	e.flushObstacleMemories(context.Background(), "s", state)
	// Gate refusals are routing decisions, never obstacles.
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://x.com/a"}`),
		"ok:false\n[闸口 G-305 实时票务未接入] 不要用网页数据冒充。", state)
	if len(state.pendingObstacleHosts) != 0 {
		t.Fatalf("gate refusal queued as obstacle: %v", state.pendingObstacleHosts)
	}
	// No session→project mapping: write skipped, never panics.
	e.memories = &layerMemoryStub{}
	e.sessions = sessionGetStub{}
	e.observeDatasourceObstacle(context.Background(), "s", "web.fetch",
		json.RawMessage(`{"url":"https://aiqicha.baidu.com/y"}`),
		"ok:false\nurl: https://aiqicha.baidu.com/y\nHTTP 403", state)
	e.flushObstacleMemories(context.Background(), "s", state)
}
