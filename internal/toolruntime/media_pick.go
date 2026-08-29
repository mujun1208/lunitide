package toolruntime

import (
	"encoding/json"
	"strings"
	"unicode"
)

type mediaUINode struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

func parseObserveNodes(output string) []mediaUINode {
	start := strings.Index(output, "{")
	if start < 0 {
		return nil
	}
	var payload struct {
		Nodes []mediaUINode `json:"nodes"`
	}
	dec := json.NewDecoder(strings.NewReader(output[start:]))
	if dec.Decode(&payload) != nil {
		return nil
	}
	return payload.Nodes
}

func foldMedia(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func labelAliases(label string) []string {
	f := foldMedia(label)
	if f == "" {
		return nil
	}
	switch f {
	case "证件号码", "身份证号码", "身份证号":
		return []string{"证件号码", "身份证号码"}
	case "手机号", "电话号码", "电话", "联系电话":
		return []string{"手机号", "电话号码", "联系电话"}
	default:
		return nil
	}
}

func labelsMatch(want, got string) bool {
	g, w := foldMedia(got), foldMedia(want)
	if g == "" || w == "" {
		return false
	}
	g = strings.TrimRight(g, "：:")
	w = strings.TrimRight(w, "：:")
	if g == w || strings.Contains(g, w) || (len([]rune(g)) >= 2 && strings.Contains(w, g)) {
		return true
	}
	for _, alias := range labelAliases(want) {
		af := foldMedia(alias)
		if g == af || strings.Contains(g, af) || strings.Contains(af, g) {
			return true
		}
	}
	return false
}

func mediaNameScore(got, want string) int {
	g, w := foldMedia(got), foldMedia(want)
	if g == "" || w == "" {
		return 0
	}
	if labelsMatch(want, got) {
		if g == w {
			return 100
		}
		return 70
	}
	if g == w {
		return 100
	}
	if strings.Contains(g, w) {
		return 70
	}
	if strings.Contains(w, g) && len([]rune(g)) >= 2 {
		return 40
	}
	return 0
}

func isGenericMediaQuery(query string) bool {
	switch foldMedia(query) {
	case "热门", "随便", "任意", "随机", "推荐", "popular", "random", "hot",
		"随便一首", "随机一首", "一首歌", "任意一首":
		return true
	default:
		return false
	}
}

func isPlayControlName(name string) bool {
	n := foldMedia(name)
	if n == "" {
		return false
	}
	if strings.Contains(n, "暂停") || n == "pause" || n == "stop" || strings.Contains(n, "停止") {
		return false
	}
	if strings.Contains(n, "列表") || strings.Contains(n, "队列") || strings.Contains(n, "历史") || strings.Contains(n, "全部") {
		return false
	}
	if strings.Contains(n, "随机") || n == "shuffle" || strings.Contains(n, "shuffle") {
		return true
	}
	switch n {
	case "播放", "play", "开始播放", "播放/暂停", "play/pause", "play pause":
		return true
	}
	return false
}

func isPauseControlName(name string) bool {
	n := foldMedia(name)
	if n == "" {
		return false
	}
	return n == "暂停" || n == "pause" || strings.HasPrefix(n, "暂停") || strings.HasPrefix(n, "pause")
}

func pickPlayControl(nodes []mediaUINode) *mediaUINode {
	var shuffle, play *mediaUINode
	bestY := -1
	for i := range nodes {
		n := &nodes[i]
		if !isPlayControlName(n.Name) {
			continue
		}
		role := strings.ToLower(n.Role)
		if role != "" && role != "button" && role != "menuitem" && role != "link" {
			continue
		}
		name := foldMedia(n.Name)
		if strings.Contains(name, "随机") || strings.Contains(name, "shuffle") {
			shuffle = n
			continue
		}
		if n.Y >= bestY {
			bestY = n.Y
			play = n
		}
	}
	if shuffle != nil {
		return shuffle
	}
	return play
}

func pickPauseControl(nodes []mediaUINode) *mediaUINode {
	for i := range nodes {
		n := &nodes[i]
		if isPauseControlName(n.Name) {
			return n
		}
	}
	return nil
}

func pickRecommendNav(nodes []mediaUINode) *mediaUINode {
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		name := foldMedia(n.Name)
		score := 0
		switch {
		case name == "每日推荐" || name == "日推":
			score = 80
		case name == "推荐":
			score = 70
		case name == "发现" || name == "首页":
			score = 40
		default:
			continue
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	return best
}

// uiaTreeSparse is true when MSAA/UIA exposed nothing we can click to play:
// empty tree, or only chrome/nav names. Electron (汽水音乐) often looks like this.
func uiaTreeSparse(nodes []mediaUINode) bool {
	if len(nodes) == 0 {
		return true
	}
	if pickSearchNode(nodes) != nil {
		return false
	}
	if pickPlayControl(nodes) != nil || pickPauseControl(nodes) != nil {
		return false
	}
	if pickFirstPlayable(nodes) != nil {
		return false
	}
	return true
}

func playbackLooksPaused(nodes []mediaUINode) bool {
	if pickPauseControl(nodes) != nil {
		return false
	}
	return pickPlayControl(nodes) != nil
}

func titleLooksLikeNowPlaying(title, app string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return false
	}
	if i := strings.Index(strings.ToLower(t), "active window:"); i >= 0 {
		t = strings.TrimSpace(t[i+len("active window:"):])
		if j := strings.Index(strings.ToLower(t), "(process:"); j >= 0 {
			t = strings.TrimSpace(t[:j])
		}
	}
	app = strings.TrimSpace(app)
	if app != "" && foldMedia(t) == foldMedia(app) {
		return false
	}
	for _, sep := range []string{" - ", " — ", " – "} {
		if strings.Contains(t, sep) {
			return true
		}
	}
	return false
}

// playbackLooksActive is the OpenClaw verify step: pause control, or a
// now-playing window title. A visible 播放 button means still paused.
func playbackLooksActive(nodes []mediaUINode, windowTitle, app string) bool {
	if playbackLooksPaused(nodes) {
		return false
	}
	if pickPauseControl(nodes) != nil {
		return true
	}
	return titleLooksLikeNowPlaying(windowTitle, app)
}

func isMediaNavName(name string) bool {
	n := foldMedia(name)
	if n == "" {
		return true
	}
	switch n {
	case "我喜欢的音乐", "喜欢", "推荐", "首页", "发现", "播客", "电台", "视频",
		"设置", "搜索", "search", "我的", "歌单", "排行榜", "每日推荐", "音乐库",
		"播放", "暂停", "下一首", "上一首", "play", "pause", "next", "previous":
		return true
	}
	return false
}

func clipMediaName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return name
}

func pickTrackNode(nodes []mediaUINode, query string) *mediaUINode {
	query = strings.TrimSpace(query)
	if query == "" || isGenericMediaQuery(query) {
		return pickFirstPlayable(nodes)
	}
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		if isMediaNavName(n.Name) && mediaNameScore(n.Name, query) < 100 {
			continue
		}
		score := mediaNameScore(n.Name, query)
		if score == 0 {
			continue
		}
		switch strings.ToLower(n.Role) {
		case "listitem", "treeitem":
			score += 12
		case "button", "link":
			score += 4
		}
		if n.H >= 28 && n.H <= 80 {
			score += 3
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if bestScore < 40 {
		return nil
	}
	return best
}

func pickFirstPlayable(nodes []mediaUINode) *mediaUINode {
	var best *mediaUINode
	bestY := -1
	for i := range nodes {
		n := &nodes[i]
		if isMediaNavName(n.Name) {
			continue
		}
		role := strings.ToLower(n.Role)
		if role != "listitem" && role != "treeitem" && role != "button" && role != "link" {
			continue
		}
		if len([]rune(n.Name)) < 2 {
			continue
		}
		if n.Y >= bestY {
			bestY = n.Y
			best = n
		}
	}
	return best
}

func pickSearchNode(nodes []mediaUINode) *mediaUINode {
	var named *mediaUINode
	namedScore := 0
	var topEdit *mediaUINode
	topY := int(^uint(0) >> 1)
	for i := range nodes {
		n := &nodes[i]
		role := strings.ToLower(n.Role)
		name := foldMedia(n.Name)
		if strings.Contains(name, "搜索") || strings.Contains(name, "search") ||
			strings.Contains(name, "查找") || strings.Contains(name, "find") ||
			strings.Contains(name, "输入") || name == "搜" {
			score := 50
			if role == "edit" || role == "combobox" {
				score += 20
			}
			if score > namedScore {
				namedScore = score
				named = n
			}
		}
		if role == "edit" || role == "combobox" {
			if n.Y < topY {
				topY = n.Y
				topEdit = n
			}
		}
	}
	if named != nil {
		return named
	}
	return topEdit
}

func nowPlayingConfirmed(nodes []mediaUINode, windowTitle, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	if playbackLooksPaused(nodes) {
		return false
	}
	if isGenericMediaQuery(query) {
		return playbackLooksActive(nodes, windowTitle, "")
	}
	if mediaNameScore(windowTitle, query) >= 70 {
		return true
	}
	maxY := 0
	for _, n := range nodes {
		bottom := n.Y + n.H
		if bottom > maxY {
			maxY = bottom
		}
	}
	barFloor := 0
	if maxY > 80 {
		barFloor = maxY * 3 / 4
	}
	for _, n := range nodes {
		if mediaNameScore(n.Name, query) < 70 {
			continue
		}
		role := strings.ToLower(n.Role)
		if maxY > 80 && n.Y >= barFloor {
			return true
		}
		if role == "listitem" || role == "treeitem" {
			continue
		}
		if role == "status" || role == "text" || role == "statictext" || role == "document" {
			if maxY <= 80 || n.Y >= maxY/2 {
				return true
			}
		}
	}
	return false
}

func summarizeNodeNames(nodes []mediaUINode, max int) string {
	if max <= 0 {
		max = 12
	}
	names := make([]string, 0, max)
	seen := map[string]bool{}
	for _, n := range nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if len(names) >= max {
			break
		}
	}
	if len(names) == 0 {
		return "（无可用控件名）"
	}
	return strings.Join(names, "、")
}

func looksPrintableQuery(s string) bool {
	for _, r := range s {
		if unicode.IsPrint(r) {
			return true
		}
	}
	return false
}

func queryLooksLikeArtist(query string) bool {
	q := strings.TrimSpace(query)
	if isGenericMediaQuery(q) {
		return false
	}
	n := len([]rune(q))
	if n < 2 || n > 16 {
		return false
	}
	return !strings.ContainsAny(q, "《》")
}

// queryMustSearchFirst is true for short artist names (周杰伦, 林俊杰).
// Those must go through the desktop search box — never treat the artist
// name as an on-screen track click, and never play with media keys only.
func queryMustSearchFirst(query string) bool {
	if !queryLooksLikeArtist(query) {
		return false
	}
	return len([]rune(strings.TrimSpace(query))) <= 4
}
