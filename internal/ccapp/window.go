package ccapp

import "strings"

// WindowInfo is one top-level visible window the companion can focus or
// screenshot. After a capture, cc.window_list projects bounds into that
// image's pixels (space=image); otherwise they are origin-relative desktop
// pixels (space=screen), matching cc.mouse_move before the first capture.
type WindowInfo struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Process    string `json:"process"`
	Class      string `json:"class,omitempty"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	W          int    `json:"w"`
	H          int    `json:"h"`
	Foreground bool   `json:"foreground"`
}

// UINode is one actionable accessibility node, with bounds converted into
// the latest screenshot's image pixel space (same coordinates as
// cc.mouse_click). Huge trees are never dumped: the host already filtered
// to actionable controls with a hard cap. Unnamed nodes keep a role label.
type UINode struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

type uiHit struct {
	ID   string
	Name string
	SX   int
	SY   int
}

// ProtectedDesktopProcess reports OS / shell processes that computer-control
// must never close, hide, or quit. Minimize / restore / move of the same
// windows stays allowed so the companion can still arrange the desktop.
func ProtectedDesktopProcess(process string) bool {
	name := strings.ToLower(strings.TrimSpace(process))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "consent", "explorer", "dwm", "winlogon", "csrss", "lsass",
		"services", "smss", "wininit", "lunitide", "lsaiso", "fontdrvhost",
		"sihost", "runtimebroker", "searchhost", "shellexperiencehost":
		return true
	}
	return false
}

// OpenWindowHints returns process stems and title fragments of the
// currently visible top-level windows. Routing uses it as a live app
// vocabulary ("在 Obsidian 里…" routes to desktop control when Obsidian is
// open even if no static table knows it). Read-only: it never enters the
// execution fence, never mutates capture state, and is safe before arming.
func (s *Service) OpenWindowHints() []string {
	if s == nil || s.host == nil || !s.host.Available() {
		return nil
	}
	wins, err := s.host.ListWindows()
	if err != nil {
		return nil
	}
	return WindowVocabulary(wins)
}

// WindowVocabulary extracts routing hints from a window list. Pure so tests
// can pin it without a host.
func WindowVocabulary(wins []WindowInfo) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		v := strings.TrimSpace(raw)
		if v == "" {
			return
		}
		n := len([]rune(v))
		if n < 2 || n > 40 {
			return
		}
		key := strings.ToLower(v)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
	}
	for _, w := range wins {
		if ProtectedDesktopProcess(w.Process) {
			continue
		}
		stem := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(w.Process)), ".exe")
		if stem != "" && len(stem) >= 3 {
			add(stem)
		}
		split := false
		for _, sep := range []string{" - ", " – ", " — ", " | ", "｜"} {
			if strings.Contains(w.Title, sep) {
				parts := strings.Split(w.Title, sep)
				// The app name is conventionally the trailing fragment
				// ("文档1 - Word", "Inbox - Outlook", "GitHub - Chrome").
				add(parts[len(parts)-1])
				split = true
			}
		}
		// Short bare titles are the app itself ("微信", "飞书", "钉钉").
		if !split && len([]rune(strings.TrimSpace(w.Title))) <= 12 {
			add(w.Title)
		}
	}
	return out
}

func chromeCloseName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimRight(n, "….")
	switch n {
	case "关闭", "close", "关闭窗口", "关闭文档", "关闭文件", "关闭程序", "退出", "exit":
		return true
	}
	return strings.HasPrefix(n, "关闭") || strings.HasPrefix(n, "close ")
}

// ChromeCloseControl is a title-bar or document-close affordance. Clicking
// it closes the user's work; refuse unless they asked to close.
func ChromeCloseControl(name string, y, w, h int) bool {
	if !chromeCloseName(name) {
		return false
	}
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "关闭窗口", "关闭文档", "关闭文件", "关闭程序", "退出", "exit":
		return true
	}
	if h > 0 && h <= 44 && w <= 96 && y < 56 {
		return true
	}
	return n == "关闭" || n == "close"
}

func documentEditorProcess(process string) bool {
	switch processStem(process) {
	case "winword", "excel", "powerpnt", "onenote", "wordpad", "notepad",
		"wps", "et", "wpp", "wpsoffice", "wpscloudsvr", "soffice", "swriter", "scalc":
		return true
	}
	return false
}

func processStem(process string) string {
	name := strings.ToLower(strings.TrimSpace(process))
	return strings.TrimSuffix(strings.TrimSuffix(name, ".lnk"), ".exe")
}

func browserProcessStem(process string) bool {
	switch processStem(process) {
	case "chrome", "msedge", "firefox":
		return true
	}
	return false
}

// pickUserFacingWindow chooses a window the user can see that is not Lunitide.
// A browser close only matches Chrome, Edge, or Firefox. A document close only
// matches an editor. WebView hosts are skipped so the companion window stays up.
func pickUserFacingWindow(wins []WindowInfo, browser bool) (WindowInfo, bool) {
	var fallback WindowInfo
	found := false
	for _, w := range wins {
		if ProtectedDesktopProcess(w.Process) || strings.Contains(processStem(w.Process), "webview") {
			continue
		}
		title := strings.ToLower(w.Title)
		if strings.Contains(title, "lunitide") || strings.Contains(w.Title, "月伴") {
			continue
		}
		if browser {
			if browserProcessStem(w.Process) {
				if w.Foreground {
					return w, true
				}
				if !found {
					fallback, found = w, true
				}
			}
			continue
		}
		if !documentEditorProcess(w.Process) {
			continue
		}
		if w.Foreground {
			return w, true
		}
		if !found {
			fallback, found = w, true
		}
	}
	return fallback, found
}

// windowFocusQuery resolves cc.window_focus args: title substring or process
// fragment (either field may be set; title wins when both are present).
func windowFocusQuery(title, process string) string {
	if q := strings.TrimSpace(title); q != "" {
		return q
	}
	return strings.TrimSpace(process)
}

func windowQueryScore(w WindowInfo, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	if strings.EqualFold(query, "foreground") {
		if w.Foreground {
			return 100
		}
		return 0
	}
	if strings.EqualFold(w.ID, query) {
		return 120
	}
	title := strings.ToLower(w.Title)
	proc := strings.ToLower(w.Process)
	stem := processStem(w.Process)
	qstem := strings.TrimSuffix(strings.TrimSuffix(query, ".lnk"), ".exe")
	score := 0
	switch {
	case title == query || proc == query || stem == qstem:
		score = 100
	case strings.Contains(title, query) || strings.Contains(proc, query) || strings.Contains(proc, qstem):
		score = 50
	}
	if score > 0 && w.Foreground {
		score++
	}
	return score
}

// MatchWindow picks the best visible window for a title / process / id query.
func MatchWindow(wins []WindowInfo, query string) (WindowInfo, bool) {
	query = strings.TrimSpace(query)
	var best WindowInfo
	bestScore := 0
	found := false
	for _, w := range wins {
		score := windowQueryScore(w, query)
		if score > bestScore {
			bestScore = score
			best = w
			found = true
		}
	}
	if !found || bestScore == 0 {
		return WindowInfo{}, false
	}
	return best, true
}

// MatchWindows returns every visible window that matches the query (for quit).
func MatchWindows(wins []WindowInfo, query string) []WindowInfo {
	query = strings.TrimSpace(query)
	var out []WindowInfo
	for _, w := range wins {
		if windowQueryScore(w, query) >= 50 {
			out = append(out, w)
		}
	}
	return out
}

func destructiveWindowOp(op string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "close", "hide":
		return true
	}
	return false
}

// SplitMenuPath turns "File > Save" / "文件/保存" into trimmed segments.
func SplitMenuPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	path = strings.ReplaceAll(path, "→", ">")
	path = strings.ReplaceAll(path, "➜", ">")
	sep := ">"
	if !strings.Contains(path, ">") && strings.Contains(path, "/") {
		sep = "/"
	}
	raw := strings.Split(path, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
