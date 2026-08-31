package app

import (
	"strings"
	"unicode"
)

func ParseTurnMentions(text string) []TurnMention {
	var out []TurnMention
	seen := map[string]bool{}
	add := func(kind, id, name string) {
		kind, id, name = strings.TrimSpace(kind), strings.TrimSpace(id), strings.TrimSpace(name)
		if kind == "" || (id == "" && name == "") {
			return
		}
		key := kind + "\x00" + id + "\x00" + name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, TurnMention{Kind: kind, ID: id, Name: name})
	}
	scanPrefixedRefs(text, "[引用专家 ", "expert", add)
	scanPrefixedRefs(text, "[引用技能 ", "skill", add)
	for _, name := range scanAtNames(text) {
		add("member", "", name)
	}
	return out
}

func scanPrefixedRefs(text, prefix, kind string, add func(kind, id, name string)) {
	rest := text
	for {
		i := strings.Index(rest, prefix)
		if i < 0 {
			return
		}
		rest = rest[i+len(prefix):]
		bar := strings.IndexByte(rest, '|')
		end := strings.IndexByte(rest, ']')
		if bar < 0 || end < 0 || bar >= end {
			continue
		}
		add(kind, strings.TrimSpace(rest[bar+1:end]), strings.TrimSpace(rest[:bar]))
		if end+1 >= len(rest) {
			return
		}
		rest = rest[end+1:]
	}
}

func scanAtNames(text string) []string {
	var names []string
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if i > 0 && !unicode.IsSpace(runes[i-1]) {
			continue
		}
		j := i + 1
		for j < len(runes) && !unicode.IsSpace(runes[j]) && runes[j] != '@' {
			j++
		}
		if j == i+1 {
			continue
		}
		names = append(names, string(runes[i+1:j]))
		i = j - 1
	}
	return names
}

func extractExpertRefNames(text string) []string {
	var names []string
	seen := map[string]bool{}
	for _, m := range ParseTurnMentions(text) {
		if m.Kind != "expert" || m.Name == "" || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		names = append(names, m.Name)
	}
	return names
}
