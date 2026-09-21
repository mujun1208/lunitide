package producthub

import "strings"

func ruleTags(c Card) []string {
	var out []string
	key := c.StableKey
	switch {
	case strings.Contains(key, ".music.") || strings.Contains(key, ".media."):
		out = append(out, "scenario:娱乐", "capability:媒体")
	case strings.Contains(key, ".mro."):
		out = append(out, "scenario:机务")
	case strings.Contains(key, ".office.") || strings.Contains(key, ".meetings."):
		out = append(out, "scenario:办公")
	case strings.Contains(key, ".command.") || strings.Contains(key, ".workspace.") || strings.Contains(key, ".agenthub."):
		out = append(out, "scenario:开发")
	}
	if strings.Contains(key, ".companion.") || strings.Contains(key, ".music.") || strings.Contains(key, "meetings.start") {
		out = append(out, "capability:语音", "entry:语音")
	}
	if strings.Contains(key, ".computer.") || strings.Contains(key, "app.launch") || strings.Contains(key, "music.open-player") {
		out = append(out, "capability:控制")
	}
	if strings.Contains(key, "memory.recall") || strings.Contains(key, "mro.search-manual") {
		out = append(out, "capability:检索")
	}
	if strings.Contains(key, ".chat.") {
		out = append(out, "entry:打字")
	}
	if strings.Contains(key, ".page.") || strings.Contains(key, ".settings.") {
		out = append(out, "entry:菜单")
	}
	if strings.HasPrefix(key, "landscape.") {
		out = append(out, "status:实验")
	} else if c.Provenance == "seed" || c.Provenance == "seed+live" || strings.Contains(key, ".page.") {
		out = append(out, "status:核心")
	}
	if strings.Contains(key, ".media.move") || strings.Contains(key, ".media.remove") || strings.Contains(key, ".media.clear") {
		out = append(out, "status:增强")
	}
	return uniqueStrings(out)
}

func applyTags(cards []Card, manual []NodeTag) []Card {
	manualBy := map[string][]string{}
	for _, t := range manual {
		if t.AssignedBy != "manual" {
			continue
		}
		manualBy[t.StableKey] = append(manualBy[t.StableKey], t.Vocab+":"+t.Value)
	}
	out := make([]Card, len(cards))
	for i, c := range cards {
		tags := append(ruleTags(c), c.Tags...)
		tags = append(tags, manualBy[c.StableKey]...)
		c.Tags = uniqueStrings(tags)
		out[i] = c
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func collectTagValues(cards []Card) []string {
	var out []string
	for _, c := range cards {
		out = append(out, c.Tags...)
	}
	return uniqueStrings(out)
}
