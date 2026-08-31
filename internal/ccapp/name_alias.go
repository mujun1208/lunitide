package ccapp

import "strings"

var chineseDigits = []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

func nameAliases(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	out := []string{name}
	runes := []rune(name)
	if len(runes) != 1 {
		return out
	}
	r := runes[0]
	if r >= '0' && r <= '9' {
		out = append(out, chineseDigits[r-'0'])
	}
	for i, d := range chineseDigits {
		if name == d {
			out = append(out, string(rune('0'+i)))
			break
		}
	}
	return out
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
