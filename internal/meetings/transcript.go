package meetings

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	acronymBRD  = regexp.MustCompile(`(?i)\bb\s*r\s*d\b`)
	acronymPRD  = regexp.MustCompile(`(?i)\bp\s*r\s*d\b`)
	acronymOKR  = regexp.MustCompile(`(?i)\bo\s*k\s*r\b`)
	acronymKPI  = regexp.MustCompile(`(?i)\bk\s*p\s*i\b`)
	acronymAPI  = regexp.MustCompile(`(?i)\ba\s*p\s*i\b`)
	yuexi       = regexp.MustCompile(`岳西|越席|月西|悦溪|跃溪|月息|悦西|悦希|月希|月夕|月惜|越汐`)
	thenThen    = regexp.MustCompile(`(?:然后){2,}`)
	erFillers   = regexp.MustCompile(`呃+`)
	enFillers   = regexp.MustCompile(`嗯+`)
	onlyFillers = regexp.MustCompile(`^(?:嗯+|啊+|呃+|那个|然后)+[。！？!?，,、\s]*$`)
	thenComma   = regexp.MustCompile(`([^ \t。！？!?，,；;：:])(然后|但是|所以|不过|而且|另外)`)
)

var keepAhAfter = map[rune]bool{
	'好': true, '是': true, '对': true, '行': true, '可': true,
	'吧': true, '呢': true, '嘛': true, '哦': true, '呀': true,
}

// CleanTranscript is a light pass over ASR text before summarize: drop oral
// fillers, restore common acronyms, keep meaning. The stored 全文逐字稿 is
// unchanged; this copy is only what the model reads.
func CleanTranscript(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if cleaned := cleanTranscriptLine(line); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return strings.Join(out, "\n")
}

func cleanTranscriptLine(raw string) string {
	text := strings.Join(strings.Fields(raw), " ")
	if text == "" {
		return ""
	}
	text = yuexi.ReplaceAllString(text, "月汐")
	text = acronymBRD.ReplaceAllString(text, "BRD")
	text = acronymPRD.ReplaceAllString(text, "PRD")
	text = acronymOKR.ReplaceAllString(text, "OKR")
	text = acronymKPI.ReplaceAllString(text, "KPI")
	text = acronymAPI.ReplaceAllString(text, "API")
	text = thenThen.ReplaceAllString(text, "然后")
	text = erFillers.ReplaceAllString(text, "")
	text = enFillers.ReplaceAllString(text, "")
	text = stripAh(text)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || onlyFillers.MatchString(text) {
		return ""
	}
	text = punctuateLine(text)
	return strings.Trim(text, "，,、")
}

func stripAh(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(rs); i++ {
		if rs[i] != '啊' {
			b.WriteRune(rs[i])
			continue
		}
		if i > 0 && keepAhAfter[rs[i-1]] {
			b.WriteRune('啊')
		}
		for i+1 < len(rs) && rs[i+1] == '啊' {
			i++
		}
	}
	return b.String()
}

func punctuateLine(text string) string {
	out := thenComma.ReplaceAllString(text, "$1，$2")
	if utf8.RuneCountInString(out) >= 8 && !strings.HasSuffix(out, "。") && !strings.HasSuffix(out, "！") && !strings.HasSuffix(out, "？") && !strings.HasSuffix(out, "!") && !strings.HasSuffix(out, "?") && !strings.HasSuffix(out, "…") {
		out += "。"
	}
	return out
}
