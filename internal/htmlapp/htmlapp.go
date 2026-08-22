// Package htmlapp renders built-in single-file playable HTML apps.
// Models must not dump a full game into workspace.write: chat MaxTokens
// cannot hold a World Cup mini-game as one tool-call argument, and the
// stream then dies with “出错了，无法完成。”
package htmlapp

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

const maxTitleRunes = 80

// Render returns a self-contained HTML document for a named template.
func Render(templateName, title string) (string, error) {
	switch strings.TrimSpace(templateName) {
	case "penalty-shootout", "":
		return renderPenalty(title), nil
	default:
		return "", fmt.Errorf("unknown html.gen template %q (supported: penalty-shootout)", templateName)
	}
}

func safeTitle(title, fallback string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		t = fallback
	}
	if utf8.RuneCountInString(t) > maxTitleRunes {
		t = string([]rune(t)[:maxTitleRunes])
	}
	return html.EscapeString(t)
}
