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
	return collapseRepeatedLines(strings.Join(out, "\n"))
}

func collapseRepeatedLines(raw string) string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		text := collapseConcatenatedRepeats(strings.TrimSpace(line))
		if text == "" {
			continue
		}
		if len(lines) == 0 {
			lines = append(lines, text)
			continue
		}
		last := lines[len(lines)-1]
		if text == last || strings.HasPrefix(last, text) && utf8.RuneCountInString(text) >= 4 {
			continue
		}
		if strings.HasPrefix(text, last) && utf8.RuneCountInString(last) >= 4 {
			lines[len(lines)-1] = text
			continue
		}
		if rest, skip := peelMeetingPrefix(last, text); skip {
			continue
		} else if rest != "" && rest != text {
			lines = append(lines, rest)
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

const minRepeatUnit = 12

func trailingSentenceMark(s string) string {
	for _, mark := range []string{"。", "！", "？", "!", "?"} {
		if strings.HasSuffix(s, mark) {
			return mark
		}
	}
	return ""
}

func collapseTandemBody(s string) string {
	rs := []rune(s)
	n := len(rs)
	if n < minRepeatUnit*2 {
		return s
	}
	for n >= minRepeatUnit*2 && n%2 == 0 && string(rs[:n/2]) == string(rs[n/2:]) {
		rs = rs[:n/2]
		n = len(rs)
	}
	s = string(rs)
	if n < minRepeatUnit*2 {
		return s
	}
	head := rs[:minRepeatUnit]
	idx := runeIndexOf(rs, head, minRepeatUnit)
	if idx < 0 {
		return s
	}
	period := idx
	if period < minRepeatUnit || n < period*2 {
		return s
	}
	unit := string(rs[:period])
	i, copies := 0, 0
	for i+period <= n && string(rs[i:i+period]) == unit {
		i += period
		copies++
	}
	if copies >= 2 && (i == n || strings.HasPrefix(unit, string(rs[i:]))) {
		return unit
	}
	return s
}

func collapseRepeatedSentences(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var out []string
	start := 0
	rs := []rune(s)
	flush := func(end int, delim string) {
		text := strings.TrimSpace(string(rs[start:end]))
		if text == "" {
			return
		}
		item := text + delim
		if len(out) > 0 {
			prev := strings.TrimRight(out[len(out)-1], "。！？!?\n")
			if prev == text {
				return
			}
			if strings.HasPrefix(prev, text) && utf8.RuneCountInString(text) >= 8 {
				return
			}
			if strings.HasPrefix(text, prev) && utf8.RuneCountInString(prev) >= 8 {
				out[len(out)-1] = item
				return
			}
		}
		out = append(out, item)
	}
	for i, r := range rs {
		if r == '。' || r == '！' || r == '？' || r == '!' || r == '?' {
			flush(i, string(r))
			start = i + 1
		}
	}
	if start < len(rs) {
		flush(len(rs), "")
	}
	return strings.Join(out, "")
}

func collapseConcatenatedRepeats(raw string) string {
	sentences := collapseRepeatedSentences(strings.TrimSpace(raw))
	mark := trailingSentenceMark(sentences)
	body := sentences
	if mark != "" {
		body = strings.TrimSuffix(sentences, mark)
	}
	collapsed := collapseTandemBody(body)
	if collapsed == body {
		return sentences
	}
	if mark != "" && !strings.HasSuffix(collapsed, mark) {
		return collapsed + mark
	}
	return collapsed
}

func runeIndexOf(hay, needle []rune, from int) int {
	if len(needle) == 0 || from > len(hay)-len(needle) {
		return -1
	}
	for i := from; i <= len(hay)-len(needle); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// peelMeetingPrefix keeps only the new clause when a later ASR dump starts
// with the previous segment, including when the previous copy has a trailing 。
func peelMeetingPrefix(prev, text string) (string, bool) {
	if text == "" {
		return "", true
	}
	if prev == "" || text == prev {
		return text, false
	}
	candidates := []string{prev, strings.TrimRight(prev, "。！？!?，,、；; \t")}
	for _, prefix := range candidates {
		if prefix == "" || utf8.RuneCountInString(prefix) < 4 {
			continue
		}
		if text == prefix || strings.HasPrefix(prefix, text) {
			return "", true
		}
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimLeft(strings.TrimPrefix(text, prefix), "，,、。.!！？? \t")
			return rest, rest == ""
		}
	}
	return text, false
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
