package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

var errCompanionToolDenied = errors.New("月伴不能执行命令行或发送即时消息，请改在工作台会话里做")

type companionActionContext struct {
	ActiveAppName string `json:"activeAppName,omitempty"`
	ActiveAppPath string `json:"activeAppPath,omitempty"`
	Kind          string `json:"kind,omitempty"`
	DesktopActive bool   `json:"desktopActive,omitempty"`
	LastTool      string `json:"lastTool,omitempty"`
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
	if sessionID == "" {
		return
	}
	switch toolName {
	case "desktop.open":
		if !strings.HasPrefix(summary, "opened ") {
			return
		}
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
		if canon := toolruntime.CanonicalMusicApp(name); canon != "" {
			name = canon
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
			DesktopActive: true,
			LastTool:      toolName,
		})
	case "media.play":
		var a struct {
			App    string `json:"app"`
			Target string `json:"target"`
		}
		_ = json.Unmarshal(args, &a)
		name := strings.TrimSpace(a.App)
		if name == "" {
			name = toolruntime.CanonicalMusicAppFromText(summary)
		}
		if canon := toolruntime.CanonicalMusicApp(name); canon != "" {
			name = canon
		}
		if name == "" || (!looksLikeMusicAppName(name) && a.Target != "foreground") {
			return
		}
		path := ""
		if strings.HasPrefix(summary, "opened ") {
			path = strings.TrimPrefix(summary, "opened ")
			if i := strings.Index(path, ";"); i >= 0 {
				path = strings.TrimSpace(path[:i])
			}
		}
		e.saveCompanionContext(sessionID, companionActionContext{
			ActiveAppName: name,
			ActiveAppPath: path,
			Kind:          "music_app",
			LastTool:      toolName,
		})
	default:
		if isDesktopControlTool(toolName) {
			ctx := e.loadCompanionContext(sessionID)
			ctx.DesktopActive = true
			ctx.LastTool = toolName
			e.saveCompanionContext(sessionID, ctx)
		}
	}
}

func companionTurnWantsMusicPlay(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"播放", "播一首", "播歌", "放一首", "来一首", "随便", "任意", "随机", "听歌", "一首",
		"play", "song", "music",
	} {
		if strings.Contains(t, needle) || strings.Contains(strings.ToLower(t), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func companionNamedMusicApp(text string) string {
	return toolruntime.CanonicalMusicAppFromText(text)
}

func companionStripMusicFiller(s string) string {
	stripped := s
	for _, cut := range []string{
		"请你帮我", "帮我", "请你", "麻烦",
		"打开网易云音乐", "打开网易云", "打开汽水音乐", "打开qq音乐", "打开QQ音乐",
		"网易云音乐", "网易云", "汽水音乐", "qq音乐", "QQ音乐",
		"播放一首", "播一首", "放一首", "来一首", "播放", "播歌", "听歌",
		"的歌曲", "的歌", "这首歌", "歌曲",
		"桌面的", "桌面",
		"软件", "客户端",
		"搜索", "搜一下",
		"打开", "启动", "运行",
		"随便", "任意", "随机",
		"一首",
	} {
		stripped = strings.ReplaceAll(stripped, cut, "")
	}
	stripped = strings.TrimSpace(stripped)
	stripped = strings.Trim(stripped, "，,。！!？?；;：:的 ")
	return strings.TrimSpace(stripped)
}

func companionExtractAfterSearch(text string) string {
	for _, verb := range []string{"搜索", "搜一下", "搜"} {
		i := strings.Index(text, verb)
		if i < 0 {
			continue
		}
		rest := companionStripMusicFiller(text[i+len(verb):])
		n := utf8.RuneCountInString(rest)
		if n >= 2 && n <= 16 {
			return rest
		}
	}
	return ""
}

func companionExtractMusicQuery(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "热门"
	}
	if q := companionExtractAfterSearch(t); q != "" {
		return q
	}
	genericHint := false
	for _, needle := range []string{"随便", "任意", "随机"} {
		if strings.Contains(t, needle) {
			genericHint = true
			break
		}
	}
	stripped := companionStripMusicFiller(t)
	if stripped == "" {
		return "热门"
	}
	n := utf8.RuneCountInString(stripped)
	if n >= 2 && n <= 16 {
		return stripped
	}
	if genericHint || n > 24 {
		return "热门"
	}
	return stripped
}

func companionDefaultMusicQuery(text string) string {
	return companionExtractMusicQuery(text)
}

func companionPlayFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"播放", "播一首", "播歌", "放一首", "来一首", "随便", "任意", "随机", "暂停", "下一首", "上一首", "切歌",
		"play", "pause", "next", "skip",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func companionMusicQueryFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if companionPlayFollowUp(t) {
		return true
	}
	runes := []rune(t)
	if len(runes) < 2 || len(runes) > 24 {
		return false
	}
	for _, idle := range []string{
		"你好", "在吗", "谢谢", "再见", "嗯嗯", "好的", "是啊", "不是", "为什么", "怎么", "什么", "今天", "天气",
	} {
		if strings.Contains(t, idle) {
			return false
		}
	}
	return !strings.ContainsAny(t, "？?！!。，,；;：:")
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
	if ctx.DesktopActive && companionDesktopFollowUp(text) {
		return true
	}
	if ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName) {
		return companionMusicQueryFollowUp(text)
	}
	return companionPlayFollowUp(text)
}

func companionDesktopFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, idle := range []string{"你好", "在吗", "谢谢", "再见", "嗯嗯", "好的", "是啊", "不是"} {
		if t == idle {
			return false
		}
	}
	for _, needle := range []string{"继续", "接着", "再点", "下一步", "填", "点一", "帮我点", "还没", "再帮", "点击", "输入", "那个", "这个", "按钮"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return companionWantsTools(t)
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
		b.WriteString("这是音乐类软件：点名歌手/歌名时 media.play target=foreground query=歌名；要随机或没说歌时 query=热门；暂停后再继续或只说「播放」且不换歌时 media.play action=play，不要带 query，不要 computer.act。禁止 cc.screen_capture、cc.mouse_click 等看屏操作，禁止 browser、netease、qqmusic 或网页搜索。")
	} else {
		b.WriteString("用户后续要在该软件里继续操作时，优先在该前台窗口内完成，不要另开网页或无关程序。")
	}
	if companionPlayFollowUp(turnText) {
		b.WriteString(" 当前这句话是续播/搜索指令，直接 media.play target=foreground，不要 desktop.open 或打开浏览器。")
	}
	return b.String()
}

// mediaPlayQueryKeepsResume leaves action=play with an empty query empty so
// media.play hits the media key (pause-then-continue). open_and_play with no
// song still searches 热门 — that is a first play, not a resume.
func mediaPlayQueryKeepsResume(action, query string) string {
	q := strings.TrimSpace(query)
	if q != "" {
		return q
	}
	if strings.EqualFold(strings.TrimSpace(action), "open_and_play") {
		return "热门"
	}
	return ""
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
	if strings.TrimSpace(a.URL) != "" {
		return args
	}
	if target == "netease" || target == "163" || target == "cloudmusic" {
		query := mediaPlayQueryKeepsResume(action, a.Query)
		out, err := json.Marshal(map[string]string{
			"action": action, "query": query, "target": "foreground", "app": "网易云音乐",
		})
		if err != nil {
			return args
		}
		return out
	}
	if target == "qq" || target == "qqmusic" {
		query := mediaPlayQueryKeepsResume(action, a.Query)
		out, err := json.Marshal(map[string]string{
			"action": action, "query": query, "target": "foreground", "app": "QQ音乐",
		})
		if err != nil {
			return args
		}
		return out
	}
	if target == "browser" {
		return args
	}
	ctx := e.loadCompanionContext(sessionID)
	useForeground := target == "foreground" || target == "app" || target == "desktop"
	if !useForeground && (target == "" || target == "auto") {
		if ctx.ActiveAppName != "" && (ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName)) {
			useForeground = true
		} else if installed := toolruntime.FirstInstalledMusicApp(); installed != "" {
			useForeground = true
			if strings.TrimSpace(a.App) == "" {
				a.App = installed
			}
		}
	}
	if !useForeground {
		return args
	}
	query := mediaPlayQueryKeepsResume(action, a.Query)
	app := strings.TrimSpace(a.App)
	if app == "" {
		app = ctx.ActiveAppName
	}
	if app == "" {
		app = toolruntime.FirstInstalledMusicApp()
	}
	out, err := json.Marshal(map[string]string{
		"action": action,
		"query":  query,
		"target": "foreground",
		"app":    app,
	})
	if err != nil {
		return args
	}
	return out
}

func (e *Engine) companionAutoMediaPlayArgs(sessionID, goal string) (json.RawMessage, bool) {
	if !companionTurnWantsMusicPlay(goal) {
		return nil, false
	}
	ctx := e.loadCompanionContext(sessionID)
	app := strings.TrimSpace(ctx.ActiveAppName)
	if app == "" || (ctx.Kind != "music_app" && !looksLikeMusicAppName(app)) {
		app = companionNamedMusicApp(goal)
	}
	if app == "" {
		q := companionDefaultMusicQuery(goal)
		if q != "" && q != "热门" && utf8.RuneCountInString(q) >= 2 && utf8.RuneCountInString(q) <= 6 {
			app = toolruntime.FirstInstalledMusicApp()
		}
	}
	if app == "" {
		return nil, false
	}
	raw, err := json.Marshal(map[string]string{
		"action": "play",
		"query":  companionDefaultMusicQuery(goal),
		"target": "foreground",
		"app":    app,
	})
	if err != nil {
		return nil, false
	}
	return e.resolveMediaPlayArgs(sessionID, raw), true
}

func (e *Engine) companionAutoDesktopTypeArgs(sessionID, goal string) (json.RawMessage, bool) {
	args := fallbackDesktopTypeArgs(goal)
	if len(args) == 0 {
		return nil, false
	}
	return args, true
}

func (e *Engine) executeUserToolWithCompanion(ctx context.Context, mode executionMode, session, name string, args json.RawMessage, progress func(chunk string), companion bool) (toolruntime.Result, error) {
	if companion && companionDefaultDeniedTool(name) {
		return toolruntime.Result{}, errCompanionToolDenied
	}
	if name == "media.play" {
		args = e.resolveMediaPlayArgs(session, args)
	}
	if companion && approvalProfileDangerous(name) && !(ccStandingApprovedTool(name) && e.companionCcEnabled(ctx)) {
		mode = executionModeApproval
	} else if companion && companionFullDiskWrite(name) && e.fullDiskChat(mode) {
		mode = executionModeApproval
	}
	if progress != nil {
		return e.executeUserToolStreaming(ctx, mode, session, name, args, progress)
	}
	return e.executeUserTool(ctx, mode, session, name, args)
}
