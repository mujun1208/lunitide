package ccapp

import "strings"

var chineseDigits = []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// Common desktop button aliases. Exact only — "播放" must not equal "随机播放".
var controlAliases = map[string][]string{
	"play": {"播放"}, "播放": {"play"},
	"pause": {"暂停"}, "暂停": {"pause"},
	"ok": {"确定", "好"}, "确定": {"ok", "好"}, "好": {"ok", "确定"},
	"cancel": {"取消"}, "取消": {"cancel"},
	"save": {"保存"}, "保存": {"save"},
	"open": {"打开"}, "打开": {"open"},
	"close": {"关闭"}, "关闭": {"close"},
	"search": {"搜索"}, "搜索": {"search"},
	"send": {"发送"}, "发送": {"send"},
	"yes": {"是"}, "是": {"yes"},
	"no": {"否"}, "否": {"no"},
	"next": {"下一首", "下一个"}, "下一首": {"next"}, "下一个": {"next"},
	"prev": {"上一首", "上一个", "previous"}, "previous": {"上一首", "上一个", "prev"},
	"上一首": {"prev", "previous"}, "上一个": {"prev", "previous"},
}

func nameAliases(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	seen := map[string]struct{}{name: {}}
	out := []string{name}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	runes := []rune(name)
	if len(runes) == 1 {
		r := runes[0]
		if r >= '0' && r <= '9' {
			add(chineseDigits[r-'0'])
		}
		for i, d := range chineseDigits {
			if name == d {
				add(string(rune('0' + i)))
				break
			}
		}
	}
	if aliases, ok := controlAliases[strings.ToLower(name)]; ok {
		for _, a := range aliases {
			add(a)
		}
	}
	return out
}

func namesExactAlias(want, got string) bool {
	want, got = strings.TrimSpace(strings.ToLower(want)), strings.TrimSpace(strings.ToLower(got))
	if want == "" || got == "" {
		return false
	}
	if want == got {
		return true
	}
	for _, alias := range nameAliases(want) {
		if strings.ToLower(alias) == got {
			return true
		}
	}
	for _, alias := range nameAliases(got) {
		if strings.ToLower(alias) == want {
			return true
		}
	}
	return false
}

func namesEquivalent(want, got string) bool {
	want, got = strings.TrimSpace(strings.ToLower(want)), strings.TrimSpace(strings.ToLower(got))
	if want == "" || got == "" {
		return false
	}
	if want == got || strings.Contains(got, want) || strings.Contains(want, got) {
		return true
	}
	for _, alias := range nameAliases(want) {
		a := strings.ToLower(alias)
		if a == got || strings.Contains(got, a) || strings.Contains(a, got) {
			return true
		}
	}
	return false
}

func allSameUIName(nodes []UINode) bool {
	if len(nodes) == 0 {
		return true
	}
	first := strings.TrimSpace(strings.ToLower(nodes[0].Name))
	for _, n := range nodes[1:] {
		if strings.TrimSpace(strings.ToLower(n.Name)) != first {
			return false
		}
	}
	return true
}

func pickPreferredNamedHit(query string, hits []UINode) UINode {
	if len(hits) == 0 {
		return UINode{}
	}
	best := hits[0]
	bestScore := namedHitPreference(query, best)
	for _, n := range hits[1:] {
		if score := namedHitPreference(query, n); score > bestScore {
			best, bestScore = n, score
		}
	}
	return best
}

func namedHitPreference(query string, n UINode) int {
	q := strings.ToLower(strings.TrimSpace(query))
	got := strings.ToLower(strings.TrimSpace(n.Name))
	score := 0
	if got == q || namesExactAlias(query, n.Name) {
		score += 80
	}
	qr, gr := []rune(q), []rune(got)
	diff := len(gr) - len(qr)
	if diff < 0 {
		diff = -diff
	}
	if extra := 40 - diff; extra > 0 {
		score += extra
	}
	role := strings.ToLower(strings.TrimSpace(n.Role))
	switch role {
	case "button", "menuitem", "hyperlink", "tab", "checkbox":
		score += 20
	}
	area := n.W * n.H
	if area > 0 && area < 40000 {
		score += area / 400
	}
	if isTransportQuery(q) {
		score += n.Y / 20
	}
	return score
}

func isTransportQuery(q string) bool {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "播放", "play", "暂停", "pause", "下一首", "next", "上一首", "prev", "previous", "上一个", "下一个":
		return true
	default:
		return false
	}
}
