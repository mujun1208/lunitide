package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

type companionActionContext struct {
	ActiveAppName string `json:"activeAppName,omitempty"`
	ActiveAppPath string `json:"activeAppPath,omitempty"`
	Kind          string `json:"kind,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

func (e *Engine) companionContextPath(sessionID string) string {
	if e == nil || e.tools == nil || sessionID == "" {
		return ""
	}
	return filepath.Join(e.tools.WorkspaceRoot(), ".companion", sessionID+".json")
}

func (e *Engine) loadCompanionContext(sessionID string) companionActionContext {
	path := e.companionContextPath(sessionID)
	if path == "" {
		return companionActionContext{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return companionActionContext{}
	}
	var ctx companionActionContext
	if json.Unmarshal(raw, &ctx) != nil {
		return companionActionContext{}
	}
	return ctx
}

func (e *Engine) saveCompanionContext(sessionID string, ctx companionActionContext) {
	path := e.companionContextPath(sessionID)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	ctx.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(ctx)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func looksLikeMusicAppName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, hint := range []string{"音乐", "music", "汽水", "qq音乐", "qqmusic", "网易云", "cloudmusic", "spotify", "酷狗", "酷我", "咪咕"} {
		if strings.Contains(n, strings.ToLower(hint)) || strings.Contains(strings.ToLower(hint), n) {
			return true
		}
	}
	return false
}

func (e *Engine) noteCompanionToolSuccess(sessionID, toolName string, args json.RawMessage, summary string) {
	if sessionID == "" || !strings.HasPrefix(summary, "opened ") {
		return
	}
	switch toolName {
	case "desktop.open":
		var a struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(args, &a) != nil {
			return
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return
		}
		path := strings.TrimPrefix(summary, "opened ")
		kind := "app"
		if looksLikeMusicAppName(name) || looksLikeMusicAppName(filepath.Base(path)) {
			kind = "music_app"
		}
		e.saveCompanionContext(sessionID, companionActionContext{
			ActiveAppName: name,
			ActiveAppPath: path,
			Kind:          kind,
		})
	}
}

func companionPlayFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"播放", "播一首", "播歌", "放一首", "来一首", "听", "暂停", "下一首", "上一首", "切歌",
		"play", "pause", "next", "skip",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func (e *Engine) companionWantsToolsForTurn(sessionID, text string) bool {
	if companionWantsTools(text) {
		return true
	}
	if sessionID == "" || e == nil {
		return false
	}
	ctx := e.loadCompanionContext(sessionID)
	if ctx.ActiveAppName == "" {
		return false
	}
	return companionPlayFollowUp(text)
}

func (e *Engine) companionSessionInjection(sessionID, turnText string) string {
	if sessionID == "" || e == nil {
		return ""
	}
	ctx := e.loadCompanionContext(sessionID)
	if ctx.ActiveAppName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[月伴会话上下文] 本会话里用户已打开桌面软件「")
	b.WriteString(strings.TrimSpace(ctx.ActiveAppName))
	b.WriteString("」")
	if ctx.ActiveAppPath != "" {
		b.WriteString("（")
		b.WriteString(strings.TrimSpace(ctx.ActiveAppPath))
		b.WriteString("）")
	}
	b.WriteString("。")
	if ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName) {
		b.WriteString("这是音乐类软件：用户只说「播放/播歌/来一首/歌手或歌名」时，必须用 media.play（target=foreground，query=歌名或歌手），在该已打开软件里搜索并播放；禁止改用 browser、netease、qqmusic 或网页搜索。")
	} else {
		b.WriteString("用户后续要在该软件里继续操作时，优先在该前台窗口内完成，不要另开网页或无关程序。")
	}
	if companionPlayFollowUp(turnText) {
		b.WriteString(" 当前这句话是续播/搜索指令，直接 media.play target=foreground，不要 desktop.open 或打开浏览器。")
	}
	return b.String()
}

func (e *Engine) resolveMediaPlayArgs(sessionID string, args json.RawMessage) json.RawMessage {
	if sessionID == "" || len(args) == 0 {
		return args
	}
	var a struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		URL    string `json:"url"`
		Target string `json:"target"`
		App    string `json:"app"`
	}
	if json.Unmarshal(args, &a) != nil {
		return args
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		action = "play"
	}
	if action != "play" && action != "open_and_play" {
		return args
	}
	target := strings.ToLower(strings.TrimSpace(a.Target))
	if target == "browser" || target == "netease" || target == "qqmusic" || target == "qq" || target == "163" {
		return args
	}
	if strings.TrimSpace(a.URL) != "" {
		return args
	}
	ctx := e.loadCompanionContext(sessionID)
	useForeground := target == "foreground" || target == "app" || target == "desktop"
	if !useForeground && (target == "" || target == "auto") {
		if ctx.ActiveAppName != "" && strings.TrimSpace(a.Query) != "" {
			if ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName) {
				useForeground = true
			}
		}
	}
	if !useForeground || strings.TrimSpace(a.Query) == "" {
		return args
	}
	app := strings.TrimSpace(a.App)
	if app == "" {
		app = ctx.ActiveAppName
	}
	out, err := json.Marshal(map[string]string{
		"action": action,
		"query":  strings.TrimSpace(a.Query),
		"target": "foreground",
		"app":    app,
	})
	if err != nil {
		return args
	}
	return out
}

func (e *Engine) executeUserToolWithCompanion(ctx context.Context, mode executionMode, session, name string, args json.RawMessage, progress func(chunk string)) (toolruntime.Result, error) {
	if name == "media.play" {
		args = e.resolveMediaPlayArgs(session, args)
	}
	if progress != nil {
		return e.executeUserToolStreaming(ctx, mode, session, name, args, progress)
	}
	return e.executeUserTool(ctx, mode, session, name, args)
}
