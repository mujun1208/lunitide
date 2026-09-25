package toolruntime

import (
	"strings"
	"unicode"
)

// CompleteSourceLine finishes the identifier at the end of one line when
// exactly one name in the file has that prefix. Two matches produce no
// suggestion.
func CompleteSourceLine(content string, lineIndex int) (string, bool) {
	lines := strings.Split(content, "\n")
	if lineIndex < 0 || lineIndex >= len(lines) {
		return "", false
	}
	line := lines[lineIndex]
	token, start := trailingIdent(line)
	if len(token) < 2 {
		return "", false
	}
	lines[lineIndex] = line[:start] + line[start+len(token):]
	idents := sourceIdents(strings.Join(lines, "\n"))
	known := make(map[string]bool, len(idents))
	for _, name := range idents {
		known[name] = true
	}
	if known[token] {
		return "", false
	}
	var hit string
	for _, name := range idents {
		if !strings.HasPrefix(name, token) {
			continue
		}
		if hit != "" && hit != name {
			return "", false
		}
		hit = name
	}
	if hit == "" {
		return "", false
	}
	return line[:start] + hit + line[start+len(token):], true
}

func applySourceLine(content string, lineIndex int, line string) string {
	lines := strings.Split(content, "\n")
	if lineIndex < 0 || lineIndex >= len(lines) {
		return content
	}
	lines[lineIndex] = line
	return strings.Join(lines, "\n")
}

func trailingIdent(line string) (string, int) {
	end := len(line)
	for end > 0 && unicode.IsSpace(rune(line[end-1])) {
		end--
	}
	start := end
	for start > 0 {
		r := rune(line[start-1])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		start--
	}
	if start == end {
		return "", 0
	}
	return line[start:end], start
}

func sourceIdents(content string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(content); {
		r := rune(content[i])
		if !unicode.IsLetter(r) && r != '_' {
			i++
			continue
		}
		j := i + 1
		for j < len(content) {
			c := rune(content[j])
			if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
				break
			}
			j++
		}
		name := content[i:j]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = j
	}
	return out
}
