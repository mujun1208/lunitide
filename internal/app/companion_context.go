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

var errCompanionToolDenied = errors.New("请通过 computer.act 调用桌面动作")

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
		"播放", "播一首", "播歌", "放一首", "放首歌", "放歌", "来一首", "随便", "任意", "随机", "听歌", "一首",
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
	if action, resume := companionMediaCommand(text); resume || (action != "" && action != "play") {
		return ""
	}
	t := strings.TrimSpace(text)
	parts := strings.FieldsFunc(t, func(r rune) bool { return strings.ContainsRune("，,。！!；;\n", r) })
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if spokenResultReportOnly(part) || strings.HasPrefix(part, "核对播放结果") || strings.HasPrefix(part, "必须核对是否开始播放") {
			continue
		}
		kept = append(kept, part)
	}
	t = strings.Join(kept, "，")
	if t == "" {
		return "random"
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
		return "random"
	}
	n := utf8.RuneCountInString(stripped)
	if n >= 2 && n <= 16 {
		return stripped
	}
	if genericHint || n > 24 {
		return "random"
	}
	return stripped
}

func companionDefaultMusicQuery(text string) string {
	return companionExtractMusicQuery(text)
}

func companionRetryWantsAlternate(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"换一种方式", "换一个方式", "换个方式", "换种方式", "另一种方式",
		"换一种方法", "换个方法", "换种方法", "换个办法",
		"换个播放器", "换一个播放器", "another way", "try another",
	} {
		if strings.Contains(t, needle) || strings.Contains(strings.ToLower(text), needle) {
			return true
		}
	}
	return false
}

func companionRetryActionTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if companionRetryWantsAlternate(t) {
		return true
	}
	for _, needle := range []string{
		"再试", "试一试", "试一下", "再来一次", "再播", "倒是试", "你倒是", "再操作", "try again",
		"再点击", "再点", "没有成功", "点击播放",
	} {
		if strings.Contains(t, needle) || strings.Contains(strings.ToLower(t), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func companionMediaCommand(text string) (action string, resume bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", false
	}
	if strings.Contains(t, "停止播放") || strings.Contains(t, "别放了") {
		return "stop", false
	}
	if strings.Contains(t, "暂停") {
		return "pause", false
	}
	if strings.Contains(t, "上一首") {
		return "prev", false
	}
	if strings.Contains(t, "下一首") || strings.Contains(t, "切歌") ||
		(strings.Contains(t, "下一周") && (strings.Contains(t, "播放") || strings.Contains(t, "音乐"))) {
		return "next", false
	}
	if strings.Contains(t, "再点击") || strings.Contains(t, "再点") || strings.Contains(t, "没有成功") ||
		strings.Contains(t, "再播一下") || strings.Contains(t, "再播放") || strings.Contains(t, "点击播放") {
		return "play", true
	}
	return "", false
}

func companionPickAlternateMusicApp(current string, installed []string) string {
	skip := toolruntime.CanonicalMusicApp(current)
	if skip == "" {
		skip = strings.TrimSpace(current)
	}
	for _, app := range installed {
		name := toolruntime.CanonicalMusicApp(app)
		if name == "" {
			name = strings.TrimSpace(app)
		}
		if name != "" && name != skip {
			return name
		}
	}
	if skip != "" {
		return skip
	}
	if len(installed) > 0 {
		return strings.TrimSpace(installed[0])
	}
	return ""
}

func companionPlayFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if companionRetryActionTurn(t) {
		return true
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
	if companionRetryActionTurn(text) && e != nil && sessionID != "" {
		ctx := e.loadCompanionContext(sessionID)
		if ctx.LastTool != "" || ctx.DesktopActive || ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName) {
			return true
		}
		prev := e.loadTurnCheckpoint(sessionID)
		if strings.TrimSpace(prev.Goal) != "" && (companionWantsTools(prev.Goal) || companionTurnWantsMusicPlay(prev.Goal) || companionWantsDesktopControl(prev.Goal)) {
			return true
		}
	}
	if sessionID == "" || e == nil {
		return false
	}
	if e.companionFileCorrection(sessionID, text) {
		return true
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
	for _, needle := range []string{"继续", "接着", "再点", "下一步", "填", "点一", "点开", "点进", "第一条", "帮我点", "还没", "再帮", "点击", "输入", "那个", "这个", "按钮"} {
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
	if e.companionFileCorrection(sessionID, turnText) {
		return "\n[本轮文件名更正] 用户在补充上一轮打开文件的名字，请用本轮给出的名字调用 desktop.open；不要只说稍等。真实路径以工具解析结果为准，不复用旧文件名。\n"
	}
	if lookupOnlyTurn(turnText) {
		return ""
	}
	ctx := e.loadCompanionContext(sessionID)
	if named := companionNamedMusicApp(turnText); named != "" && named != toolruntime.CanonicalMusicApp(ctx.ActiveAppName) {
		return ""
	}
	if target, open := desktopOpenTargetFromGoal(turnText); open && !looksLikeMusicAppName(target) {
		return ""
	}
	if (ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName)) &&
		(looksLikeDesktopObserveTurn(turnText) || looksLikeTypeAfterLabelTurn(turnText)) {
		return ""
	}
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
		b.WriteString("这是音乐类软件：点名歌手/歌名时 media.play target=foreground query=歌名；要随机或没说歌时 query=random；暂停后再继续或只说「播放」且不换歌时 media.play action=play，不要带 query，不要 computer.act。禁止 cc.screen_capture、cc.mouse_click 等看屏操作，禁止 browser、netease、qqmusic 或网页搜索。")
	} else {
		b.WriteString("用户后续要在该软件里继续操作时，优先在该前台窗口内完成，不要另开网页或无关程序。")
	}
	if companionPlayFollowUp(turnText) && (ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName)) && companionNamedMusicApp(turnText) == "" {
		b.WriteString(" 当前这句话是续播/搜索指令，直接 media.play target=foreground，不要 desktop.open 或打开浏览器。")
	}
	return b.String()
}

func (e *Engine) companionFileCorrection(sessionID, text string) bool {
	t := strings.Trim(strings.TrimSpace(text), "。.!！？? ")
	if t == "" || len([]rune(t)) > 80 || looksLikeCurrentLookupTurn(t) || companionWantsTools(t) {
		return false
	}
	namedFile := false
	for _, suffix := range []string{"图片", "照片", "文件", "文档", ".txt", ".png", ".jpg", "TXT 文件"} {
		if strings.HasSuffix(t, suffix) && t != suffix {
			namedFile = true
			break
		}
	}
	if !namedFile {
		return false
	}
	previous := e.loadTurnCheckpoint(sessionID)
	_, open := desktopOpenTargetFromGoal(previous.Goal)
	return open
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
		return "random"
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
	if app == "" && (ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName)) {
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

func companionShouldAutoMediaPlay(goal string) bool {
	if action, _ := companionMediaCommand(goal); action != "" {
		return true
	}
	return companionTurnWantsMusicPlay(goal) || companionRetryActionTurn(goal)
}

func (e *Engine) companionAutoMediaPlayArgs(sessionID, goal string) (json.RawMessage, bool) {
	return e.companionAutoMediaPlayArgsForTurn(sessionID, goal, goal)
}

func (e *Engine) companionAutoMediaPlayArgsForTurn(sessionID, goal, spoken string) (json.RawMessage, bool) {
	hint := strings.TrimSpace(spoken)
	if hint == "" {
		hint = goal
	}
	mediaAction, mediaResume := companionMediaCommand(hint)
	if mediaAction == "" {
		mediaAction, mediaResume = companionMediaCommand(goal)
	}
	if !companionTurnWantsMusicPlay(goal) {
		if !companionRetryActionTurn(goal) && !companionRetryActionTurn(hint) && mediaAction == "" {
			return nil, false
		}
		if sessionID != "" && e != nil {
			if prev := e.loadTurnCheckpoint(sessionID); companionTurnWantsMusicPlay(prev.Goal) {
				goal = prev.Goal
			}
		}
		if !companionTurnWantsMusicPlay(goal) {
			ctx := companionActionContext{}
			if sessionID != "" && e != nil {
				ctx = e.loadCompanionContext(sessionID)
			}
			if ctx.Kind != "music_app" && !looksLikeMusicAppName(ctx.ActiveAppName) {
				return nil, false
			}
			goal = "随机播放"
		}
	}
	ctx := e.loadCompanionContext(sessionID)
	app := companionNamedMusicApp(goal)
	if app == "" && (ctx.Kind == "music_app" || looksLikeMusicAppName(ctx.ActiveAppName)) {
		app = strings.TrimSpace(ctx.ActiveAppName)
	}
	if app == "" {
		q := companionDefaultMusicQuery(goal)
		if q != "" && q != "热门" && q != "random" && utf8.RuneCountInString(q) >= 2 && utf8.RuneCountInString(q) <= 6 {
			app = toolruntime.FirstInstalledMusicApp()
		}
	}
	if companionRetryWantsAlternate(hint) {
		if next := companionPickAlternateMusicApp(app, toolruntime.InstalledMusicApps()); next != "" {
			app = next
		}
	}
	if app == "" {
		return nil, false
	}
	action := mediaAction
	resume := mediaResume
	if action == "" {
		action = "play"
	}
	query := companionDefaultMusicQuery(goal)
	if resume || action != "play" {
		query = ""
	}
	raw, err := json.Marshal(map[string]string{
		"action": action,
		"query":  query,
		"target": "foreground",
		"app":    app,
	})
	if err != nil {
		return nil, false
	}
	return e.resolveMediaPlayArgs(sessionID, raw), true
}

func mediaArgsForGoal(goal string, args json.RawMessage) json.RawMessage {
	app := companionNamedMusicApp(goal)
	if app == "" || strings.Contains(goal, "网页版") {
		return args
	}
	var fields map[string]any
	if json.Unmarshal(args, &fields) != nil {
		return args
	}
	fields["app"], fields["target"] = app, "foreground"
	action, _ := fields["action"].(string)
	if (action == "" || action == "play" || action == "open_and_play") && companionTurnWantsMusicPlay(goal) {
		q := companionDefaultMusicQuery(goal)
		if q == "热门" || q == "random" {
			fields["query"] = "random"
		}
	}
	delete(fields, "url")
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
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
	// Honor the selected execution mode on both voice and text. The shared
	// runtime still enforces workspace grants, CC enable, and emergency stop.
	if progress != nil {
		return e.executeUserToolStreaming(ctx, mode, session, name, args, progress)
	}
	return e.executeUserTool(ctx, mode, session, name, args)
}
