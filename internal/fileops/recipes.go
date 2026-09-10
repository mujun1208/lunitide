package fileops

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// ClassifyRename plans moving listed files into extension folders (pdf/, docx/, ...).
func ClassifyRename(files []string) []Item {
	var items []Item
	used := map[string]bool{}
	for _, rel := range files {
		rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
		if rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, ":") {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(rel), "."))
		if ext == "" {
			ext = "other"
		}
		base := filepath.Base(rel)
		dest := path.Join(ext, base)
		if dest == rel || used[dest] {
			continue
		}
		used[dest] = true
		items = append(items, Item{Action: ActionMove, From: rel, To: dest})
	}
	return items
}

// WeeklyReport plans a markdown week-report from already-read source notes.
func WeeklyReport(dest string, title string, notes []string) []Item {
	if dest == "" {
		dest = "reports/weekly-report.md"
	}
	if title == "" {
		title = "周报"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if len(notes) == 0 {
		b.WriteString("（没有可用的参考材料正文。请先放入材料后再生成。）\n")
	}
	for i, note := range notes {
		fmt.Fprintf(&b, "## 材料 %d\n\n%s\n\n", i+1, strings.TrimSpace(note))
	}
	return []Item{{Action: ActionWrite, To: dest, Body: b.String()}}
}

// Briefing plans a short public-info brief from supplied bullets.
func Briefing(dest string, title string, bullets []string) []Item {
	if dest == "" {
		dest = "briefs/briefing.md"
	}
	if title == "" {
		title = "资讯简报"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if len(bullets) == 0 {
		b.WriteString("（没有可用要点。）\n")
	}
	for _, bullet := range bullets {
		line := strings.TrimSpace(bullet)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "-") {
			line = "- " + line
		}
		b.WriteString(line + "\n")
	}
	return []Item{{Action: ActionWrite, To: dest, Body: b.String()}}
}

func SanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "untitled"
	}
	return out
}
