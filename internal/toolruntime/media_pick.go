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

func mediaNameScore(got, want string) int {
	g, w := foldMedia(got), foldMedia(want)
	if g == "" || w == "" {
		return 0
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
	case "热门", "随便", "任意", "随机", "推荐", "popular", "random", "hot":
		return true
	default:
		return false
	}
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
		if strings.Contains(name, "搜索") || strings.Contains(name, "search") || name == "搜" {
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
	if isGenericMediaQuery(query) {
		return true
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
