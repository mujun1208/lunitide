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
// to named controls with a hard cap.
type UINode struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
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

func processStem(process string) string {
	name := strings.ToLower(strings.TrimSpace(process))
	return strings.TrimSuffix(strings.TrimSuffix(name, ".lnk"), ".exe")
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
