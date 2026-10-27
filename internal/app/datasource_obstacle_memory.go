package app

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
)

// capability-self-bootstrap P5: datasource obstacle memory. When a web fetch
// hits a login wall or anti-crawl rejection on one host and another path in
// the same turn succeeds (typically web.search, whose engine cards carry the
// same public information), the engine settles that fact into project
// semantic memory: "this host is walled, similar queries should ride
// web.search instead". prepareChatMemory recalls project semantic memories
// as evidence, so the next similar query routes the proven way from the
// first tool call instead of re-burning the walled source — and the G-308
// budget is never spent rediscovering the wall.
const datasourceObstacleMemoryKeyPrefix = "datasource-obstacle:"

// observeDatasourceObstacle folds one web tool outcome into the turn's
// obstacle tracking. A walled web.fetch queues its host; a successful
// web.search — the alternate path that worked — flushes the queue into
// memory; a plain successful web.fetch clears its own host's entry (the
// obstacle was transient, e.g. a rate limit that recovered).
func (e *Engine) observeDatasourceObstacle(ctx context.Context, sessionID, tool string, args json.RawMessage, output string, state *streamState) {
	if e == nil || state == nil {
		return
	}
	out := strings.TrimSpace(output)
	switch tool {
	case "web.fetch":
		host := requestHostFromArgs(args)
		if host == "" {
			return
		}
		switch {
		case strings.HasPrefix(out, "ok:false"):
			// Gate refusals are routing decisions, not datasource state.
			if !strings.HasPrefix(out, "ok:false\n[闸口") && datasourceObstacleFailure(out) {
				state.queueObstacleHost(host)
			}
		case loginWallSuccessOutput(out):
			// The fetch "succeeded" by landing on an auth interstitial: the
			// requested host served its login wall instead of content.
			state.queueObstacleHost(host)
		default:
			state.clearObstacleHost(host)
		}
	case "web.search":
		if !strings.HasPrefix(out, "ok:false") {
			e.flushObstacleMemories(ctx, sessionID, state)
		}
	}
}

// flushObstacleMemories writes one project semantic memory per pending
// obstacle host. The write is gated by the auto-capture policy: obstacle
// facts are observed, not explicitly given by the user.
func (e *Engine) flushObstacleMemories(ctx context.Context, sessionID string, state *streamState) {
	if e == nil || state == nil || len(state.pendingObstacleHosts) == 0 {
		return
	}
	for host := range state.pendingObstacleHosts {
		e.recordDatasourceObstacle(ctx, sessionID, host)
	}
	state.pendingObstacleHosts = nil
}

// recordDatasourceObstacle upserts the obstacle memory for host (never
// duplicates), mirroring the MCP preset approval memory's write pattern.
func (e *Engine) recordDatasourceObstacle(ctx context.Context, sessionID, host string) {
	if e == nil || !memoryServiceAvailable(e.memories) || strings.TrimSpace(host) == "" {
		return
	}
	if !m8core.ResolveMemoryBehavior(e.chatMemoryV2(ctx), "project", m8core.CurrentProductFlags()).AllowAutoCapture {
		return
	}
	projectID := e.projectIDForSession(ctx, sessionID)
	if projectID == "" {
		return
	}
	key := datasourceObstacleMemoryKeyPrefix + host
	content := "数据源障碍：" + host + " 存在登录墙/反爬限制（直接抓取未取到公开信息，已验证 web.search 搜索引擎路径可获取同类公开信息）。" +
		"同类企业、公司、人物信息查询优先用 web.search（搜索引擎结果与企业卡片），不要直接抓取该站点。"
	src, srcType := sessionID, "obstacle-observe"
	items, err := e.memories.ListByProject(ctx, projectID, memory.LayerSemantic)
	if err != nil {
		log.Printf("obstacle memory: write skipped: %v", err)
		return
	}
	for _, item := range items {
		if item.Key == key {
			if err := e.memories.UpdateContent(ctx, item.ID, content); err != nil {
				log.Printf("obstacle memory: update skipped: %v", err)
			}
			return
		}
	}
	_, err = e.memories.Create(ctx, memory.Memory{
		ProjectID:  projectID,
		Layer:      memory.LayerSemantic,
		Scope:      memory.ScopeProject,
		Key:        key,
		Content:    content,
		SourceID:   &src,
		SourceType: &srcType,
		Confidence: 0.8,
	})
	if err != nil {
		log.Printf("obstacle memory: write skipped: %v", err)
	}
}

// requestHostFromArgs extracts the target hostname of a web.fetch call.
func requestHostFromArgs(args json.RawMessage) string {
	var a struct {
		URL string `json:"url"`
	}
	if len(args) == 0 || json.Unmarshal(args, &a) != nil || a.URL == "" {
		return ""
	}
	u, err := url.Parse(a.URL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// loginWallSuccessOutput reports whether a nominally successful web.fetch
// output actually landed on an auth interstitial: the final URL is a
// passport/login/sso host or path, or the page is a tiny titled login
// prompt. Real content pages are neither.
func loginWallSuccessOutput(out string) bool {
	if loginWallFinalURL(out) {
		return true
	}
	title := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "title: ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "title: "))
			break
		}
	}
	if title == "" {
		return false
	}
	lower := strings.ToLower(title)
	for _, marker := range []string{"登录", "login", "安全验证", "access denied", "forbidden"} {
		if strings.Contains(lower, marker) {
			body := out
			if idx := strings.Index(out, "\n\n"); idx >= 0 {
				body = out[idx+2:]
			}
			return len([]rune(strings.TrimSpace(body))) < 400
		}
	}
	return false
}

// loginWallFinalURL checks the output's "url: " line (the final URL after
// redirects) for auth interstitial markers.
func loginWallFinalURL(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "url: ") {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(strings.TrimPrefix(line, "url: ")))
		if err != nil {
			return false
		}
		host := strings.ToLower(u.Hostname())
		for _, marker := range []string{"passport.", "login.", "signin.", "wappass", "sso.", "auth."} {
			if strings.Contains(host, marker) {
				return true
			}
		}
		path := strings.ToLower(u.Path)
		for _, marker := range []string{"/login", "/signin", "/verify", "/passport", "/captcha"} {
			if path == marker || strings.HasPrefix(path, marker+"/") {
				return true
			}
		}
	}
	return false
}

func (s *streamState) queueObstacleHost(host string) {
	if s.pendingObstacleHosts == nil {
		s.pendingObstacleHosts = make(map[string]bool)
	}
	s.pendingObstacleHosts[host] = true
}

func (s *streamState) clearObstacleHost(host string) {
	delete(s.pendingObstacleHosts, host)
}
