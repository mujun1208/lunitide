package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/oklog/ulid/v2"
)

func newsOpenGoal(text string) bool {
	return openNewsGoal(text) || systemBrowserFirstResultGoal(text) || websiteFirstResultGoal(text)
}

func searchPageURL(raw string) bool {
	u := strings.ToLower(raw)
	for _, host := range []string{"bing.com/search", "google.com/search", "search.yahoo.com", "baidu.com/s", "sogou.com/web"} {
		if strings.Contains(u, host) {
			return true
		}
	}
	return false
}

func firstSearchHitURL(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		if searchPageURL(line) {
			continue
		}
		return line
	}
	return ""
}

func firstSearchHitFromMessages(messages []llmadapter.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llmadapter.RoleTool && messages[i].Role != llmadapter.RoleAssistant {
			continue
		}
		if u := firstSearchHitURL(messages[i].Content); u != "" {
			return u
		}
	}
	return ""
}

func (e *Engine) rememberSearchHit(session, summary string) {
	if e == nil || session == "" {
		return
	}
	if u := firstSearchHitURL(summary); u != "" {
		e.searchFirstHit.Store(session, u)
	}
}

func (e *Engine) savedSearchHit(session string, messages []llmadapter.Message) string {
	if e != nil && session != "" {
		if v, ok := e.searchFirstHit.Load(session); ok {
			if s, _ := v.(string); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return firstSearchHitFromMessages(messages)
}

func rewriteNewsOpen(goal, session string, e *Engine, messages []llmadapter.Message, call *llmadapter.ToolCall) {
	if call == nil || !newsOpenGoal(goal) {
		return
	}
	if call.Name != "computer.act" && !strings.HasPrefix(call.Name, "cc.") {
		return
	}
	u := e.savedSearchHit(session, messages)
	if u == "" {
		return
	}
	raw, err := json.Marshal(map[string]string{"url": u})
	if err != nil {
		return
	}
	call.Name = "web.fetch"
	call.Arguments = raw
}

func (e *Engine) finishDirectTool(send func(bridge.Event) error, name string, args json.RawMessage, summary string) (string, error) {
	callID := "direct-" + ulid.Make().String()
	digest := argsDigestOrFallback(name, args)
	if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(name, args))}}); err != nil {
		return "", err
	}
	if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(summary)}}); err != nil {
		return "", err
	}
	return callID, nil
}

const namedSongSpeech = "已打开网易云音乐的官方搜索，登录后即可播放。"

func directSongPlayArgs(goal string) json.RawMessage {
	if moviePlayGoal(goal) || !companionTurnWantsMusicPlay(goal) {
		return nil
	}
	q := strings.TrimSpace(companionDefaultMusicQuery(goal))
	if q == "" || q == "random" || q == "热门" {
		return nil
	}
	page := "https://music.163.com/#/search/m/?s=" + url.QueryEscape(q)
	raw, err := json.Marshal(map[string]string{
		"action": "open",
		"query":  q,
		"url":    page,
	})
	if err != nil {
		return nil
	}
	return raw
}

func (e *Engine) openNamedSongNow(ctx context.Context, mode executionMode, sessionID, goal string, send func(bridge.Event) error) (string, bool) {
	args := directSongPlayArgs(goal)
	if e == nil || e.tools == nil || len(args) == 0 {
		return "", false
	}
	out, err := e.executeUserTool(ctx, mode, sessionID, "media.play", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || !strings.Contains(summary, "music.163.com") {
		return "", false
	}
	if _, err := e.finishDirectTool(send, "media.play", args, summary); err != nil {
		return "", false
	}
	return namedSongSpeech, true
}

func (e *Engine) openNamedFilmNow(ctx context.Context, mode executionMode, sessionID, goal string, send func(bridge.Event) error) (string, bool) {
	if e == nil || e.tools == nil || !moviePlayGoal(goal) {
		return "", false
	}
	args := forceMediaCenterArgs(goal, nil)
	out, err := e.executeUserTool(ctx, mode, sessionID, "media.play", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || summary == "" || (!strings.Contains(summary, "iqiyi.com") && !strings.Contains(summary, "MEDIA_CENTER")) {
		return "", false
	}
	if _, err := e.finishDirectTool(send, "media.play", args, summary); err != nil {
		return "", false
	}
	if strings.Contains(summary, "iqiyi.com") {
		return "已打开爱奇艺的官方搜索，登录后即可播放。", true
	}
	return "已交给媒体中心播放。", true
}

func (e *Engine) openFirstNewsNow(sessionID, goal string, messages []llmadapter.Message, send func(bridge.Event) error) (string, bool) {
	if !newsOpenGoal(goal) {
		return "", false
	}
	u := e.savedSearchHit(sessionID, messages)
	if u == "" {
		return "", false
	}
	args, err := json.Marshal(map[string]string{"url": u})
	if err != nil {
		return "", false
	}
	summary := "url: " + u + "\nfirst_hit: true\n已打开第一条。"
	if _, err := e.finishDirectTool(send, "web.fetch", args, summary); err != nil {
		return "", false
	}
	return "已经打开第一条。", true
}

const documentSavedSpeech = "已在文档里写好并保存。"

func (e *Engine) typeIntoDocumentNow(ctx context.Context, mode executionMode, sessionID, goal string, send func(bridge.Event) error) (string, bool) {
	args := fallbackDesktopTypeArgs(goal)
	if e == nil || e.tools == nil || len(args) == 0 || !strings.Contains(string(args), `"save":true`) {
		return "", false
	}
	if openArgs := fallbackDesktopOpenArgs(goal); len(openArgs) > 0 {
		opened, err := e.executeUserTool(ctx, mode, sessionID, "desktop.open", openArgs)
		if err != nil || strings.TrimSpace(opened.Output) == "" {
			return "", false
		}
		if _, err = e.finishDirectTool(send, "desktop.open", openArgs, opened.Output); err != nil {
			return "", false
		}
	}
	out, err := e.executeUserTool(ctx, mode, sessionID, "desktop.type", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || !strings.Contains(summary, "saved") {
		return "", false
	}
	if _, err = e.finishDirectTool(send, "desktop.type", args, summary); err != nil {
		return "", false
	}
	return documentSavedSpeech, true
}

func (e *Engine) sendComposerNow(ctx context.Context, mode executionMode, sessionID, goal string, send func(bridge.Event) error) (string, bool) {
	args := composerSendTypeArgs(goal, nil)
	if e == nil || e.tools == nil || len(args) == 0 {
		return "", false
	}
	out, err := e.executeUserTool(ctx, mode, sessionID, "desktop.type", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || !strings.Contains(summary, "submitted") {
		return "", false
	}
	if _, err = e.finishDirectTool(send, "desktop.type", args, summary); err != nil {
		return "", false
	}
	var fields map[string]any
	if json.Unmarshal(args, &fields) == nil {
		if window, _ := fields["window"].(string); window != "" {
			return "已在" + window + "的输入框写好并发送。", true
		}
	}
	return "已在输入框写好并发送。", true
}

func (e *Engine) startWeChatChatNow(ctx context.Context, mode executionMode, sessionID, goal string, req *llmadapter.Request, send func(bridge.Event) error) bool {
	args := wechatChatTypeArgs(goal)
	if e == nil || e.tools == nil || req == nil || len(args) == 0 {
		return false
	}
	out, err := e.executeUserTool(ctx, mode, sessionID, "desktop.type", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || !strings.Contains(summary, "opened chat") {
		return false
	}
	callID, err := e.finishDirectTool(send, "desktop.type", args, summary)
	if err != nil {
		return false
	}
	req.Messages = append(req.Messages,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: callID, Name: "desktop.type", Arguments: args}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: callID, Content: summary},
	)
	if len(out.VisionData) > 0 {
		req.Images = appendCaptureVision(req.Images, out.VisionMIME, out.VisionData)
	}
	return true
}
