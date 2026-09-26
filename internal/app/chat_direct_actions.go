package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
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
	call.Name = "desktop.browse"
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
	if moviePlayGoal(goal) || ownedMediaCenterGoal(goal) || !companionTurnWantsMusicPlay(goal) {
		return nil
	}
	q := strings.TrimSpace(companionDefaultMusicQuery(goal))
	if q == "" || q == "random" || q == "热门" {
		return nil
	}
	raw, err := json.Marshal(map[string]string{
		"action": "play",
		"target": "center",
		"query":  strings.TrimSpace(goal),
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
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "找不到这首歌，没有这首歌。"
		}
		return msg, true
	}
	summary := strings.TrimSpace(out.Output)
	if !strings.Contains(summary, "MEDIA_CENTER") || strings.Contains(summary, "MEDIA_CENTER_STOP") {
		return "", false
	}
	if _, err := e.finishDirectTool(send, "media.play", args, summary); err != nil {
		return "", false
	}
	return mediaCenterReadySpeech(summary), true
}

func (e *Engine) openNamedFilmNow(ctx context.Context, mode executionMode, sessionID, goal string, send func(bridge.Event) error) (string, bool) {
	if e == nil || e.tools == nil || (!moviePlayGoal(goal) && !ownedMediaCenterGoal(goal)) {
		return "", false
	}
	args := forceMediaCenterArgs(goal, nil)
	out, err := e.executeUserTool(ctx, mode, sessionID, "media.play", args)
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "媒体中心没有这部片子，不会改放别的电影。"
		}
		return msg, true
	}
	summary := strings.TrimSpace(out.Output)
	if !strings.Contains(summary, "MEDIA_CENTER") || strings.Contains(summary, "MEDIA_CENTER_STOP") {
		return "", false
	}
	if _, err := e.finishDirectTool(send, "media.play", args, summary); err != nil {
		return "", false
	}
	return mediaCenterReadySpeech(summary), true
}

func mediaCenterReadySpeech(summary string) string {
	title := ""
	site := ""
	for _, line := range strings.Split(summary, "\n") {
		if rest, ok := strings.CutPrefix(line, "title: "); ok {
			title = strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(line, "site: "); ok {
			site = strings.TrimSpace(rest)
		}
	}
	if title == "" {
		if site == "" {
			return "已交给媒体中心播放。"
		}
		return "已从" + site + "找到，在媒体中心开始播放。"
	}
	if site != "" {
		return "已从" + site + "找到《" + title + "》，在媒体中心开始播放。"
	}
	return "已交给媒体中心播放《" + title + "》。"
}

func httpURLs(text string) []string {
	var out []string
	for i := 0; i < len(text); {
		rest := text[i:]
		https := strings.Index(rest, "https://")
		http := strings.Index(rest, "http://")
		if https < 0 && http < 0 {
			break
		}
		at := https
		if at < 0 || (http >= 0 && http < at) {
			at = http
		}
		chunk := rest[at:]
		end := len(chunk)
		for j, r := range chunk {
			if r <= ' ' || strings.ContainsRune("。，、；;\"'<>）)]}", r) {
				end = j
				break
			}
		}
		if end > 0 {
			out = append(out, chunk[:end])
		}
		next := at + end
		if next <= 0 {
			next = 1
		}
		i += next
	}
	return out
}

func queryFromSearchURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	for _, key := range []string{"q", "wd", "query", "p"} {
		if q := strings.TrimSpace(u.Query().Get(key)); q != "" {
			return q
		}
	}
	return ""
}

func searchQueryFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if q, ok := strings.CutPrefix(line, "query: "); ok && strings.TrimSpace(q) != "" {
			return strings.TrimSpace(q)
		}
	}
	for _, raw := range httpURLs(text) {
		if !searchPageURL(raw) {
			continue
		}
		if q := queryFromSearchURL(raw); q != "" {
			return q
		}
	}
	return ""
}

// firstOpenTarget is the link to open, or the query whose first result to open.
// The newest search wins: a later browser search page beats an older result URL.
func firstOpenTarget(messages []llmadapter.Message) (openURL, searchQuery string) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if u := firstSearchHitURL(msg.Content); u != "" {
			return u, ""
		}
		if q := searchQueryFromText(msg.Content); q != "" {
			return "", q
		}
		if msg.Role != llmadapter.RoleAssistant {
			continue
		}
		for j := len(msg.ToolCalls) - 1; j >= 0; j-- {
			call := msg.ToolCalls[j]
			if call.Name != "desktop.browse" && call.Name != "web.search" {
				continue
			}
			var a struct {
				Query string `json:"query"`
				URL   string `json:"url"`
			}
			if json.Unmarshal(call.Arguments, &a) != nil {
				continue
			}
			if q := strings.TrimSpace(a.Query); q != "" {
				return "", q
			}
			if q := queryFromSearchURL(a.URL); q != "" {
				return "", q
			}
		}
	}
	return "", ""
}

func openedSearchPage(messages []llmadapter.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		for _, raw := range httpURLs(messages[i].Content) {
			if searchPageURL(raw) {
				return raw
			}
		}
	}
	return ""
}

func looksLikeBrowserWindow(w ccapp.WindowInfo) bool {
	p := strings.ToLower(w.Process)
	return strings.Contains(p, "msedge") || strings.Contains(p, "chrome") || strings.Contains(p, "firefox")
}

func browserResultWindow(wins []ccapp.WindowInfo) (ccapp.WindowInfo, bool) {
	var found ccapp.WindowInfo
	var ok bool
	for _, w := range wins {
		if !looksLikeBrowserWindow(w) {
			continue
		}
		if w.Foreground {
			return w, true
		}
		if !ok {
			found = w
			ok = true
		}
	}
	return found, ok
}

type resultClicker interface {
	Available() bool
	ListWindows() ([]ccapp.WindowInfo, error)
	FocusWindow(query string) (ccapp.WindowInfo, error)
	ObserveUI(maxNodes int) ([]ccapp.UINode, error)
	InvokeUI(target string) error
}

var resultClickHost = func() resultClicker { return ccapp.PlatformHost() }

func clickFirstResult(host resultClicker) (string, error) {
	if host == nil || !host.Available() {
		return "", errors.New("desktop control unavailable")
	}
	wins, err := host.ListWindows()
	if err != nil {
		return "", err
	}
	win, ok := browserResultWindow(wins)
	if !ok || strings.TrimSpace(win.Title) == "" {
		return "", errors.New("browser window not open")
	}
	if _, err = host.FocusWindow(win.Title); err != nil {
		return "", err
	}
	nodes, err := host.ObserveUI(120)
	if err != nil {
		return "", err
	}
	name, ok := ccapp.FirstResultLinkName(nodes)
	if !ok {
		return "", errors.New("no result link")
	}
	if err = host.InvokeUI(name); err != nil {
		return "", err
	}
	return name, nil
}

var clickFirstBrowserResult = func(_ *Engine, _ context.Context, _ executionMode, _ string) (string, error) {
	return clickFirstResult(resultClickHost())
}

func (e *Engine) savedSearchQuery(session string) string {
	if e == nil || session == "" {
		return ""
	}
	v, ok := e.searchLastQuery.Load(session)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func (e *Engine) rememberSearchQuery(session, query string) {
	if e == nil || session == "" {
		return
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	if prev, ok := e.searchLastQuery.Load(session); ok {
		if s, _ := prev.(string); s != "" && s != query {
			e.searchFirstHit.Delete(session)
		}
	}
	e.searchLastQuery.Store(session, query)
}

var executeDirectTool = func(e *Engine, ctx context.Context, mode executionMode, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	return e.executeUserTool(ctx, mode, session, name, args)
}

func (e *Engine) openFirstNewsNow(ctx context.Context, mode executionMode, sessionID, goal string, messages []llmadapter.Message, send func(bridge.Event) error) (string, bool) {
	if e == nil || e.tools == nil || !newsOpenGoal(goal) {
		return "", false
	}
	openURL, query := firstOpenTarget(messages)
	if openURL == "" && query == "" {
		query = e.savedSearchQuery(sessionID)
	}
	if openURL == "" && query == "" {
		openURL = e.savedSearchHit(sessionID, nil)
	}
	if openURL == "" && (openedSearchPage(messages) != "" || e.desktopBrowserIsOpen(sessionID)) {
		name, err := clickFirstBrowserResult(e, ctx, mode, sessionID)
		name = strings.TrimSpace(name)
		if err == nil && name != "" {
			args, mErr := json.Marshal(map[string]string{"action": "click", "name": name})
			if mErr != nil {
				return "", false
			}
			summary := "已点击第一条：" + name + "\nfirst_hit: true\n已打开第一条。"
			if _, err = e.finishDirectTool(send, "computer.act", args, summary); err != nil {
				return "", false
			}
			return "已经打开第一条。", true
		}
	}
	if openURL == "" || searchPageURL(openURL) {
		return "", false
	}
	args, err := json.Marshal(map[string]string{"url": openURL})
	if err != nil {
		return "", false
	}
	out, err := executeDirectTool(e, ctx, mode, sessionID, "desktop.browse", args)
	summary := strings.TrimSpace(out.Output)
	if err != nil || (!strings.HasPrefix(summary, "已打开桌面浏览器：") && !strings.HasPrefix(summary, "已向系统默认桌面浏览器发送打开请求")) {
		return "", false
	}
	summary += "\nfirst_hit: true\n已打开第一条。"
	if _, err = e.finishDirectTool(send, "desktop.browse", args, summary); err != nil {
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
