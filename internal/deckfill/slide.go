package deckfill

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	szAttr = regexp.MustCompile(`\bsz="(\d+)"`)
	cxAttr = regexp.MustCompile(`<a:ext\b[^>]*\bcx="(\d+)"`)
)

type run struct{ start, end int }

// fillSlide replaces sample copy in document order.
// Unused prompt slots are cleared so leftover「添加标题」does not survive.
// Shapes classified as SlotConfirm are left unchanged.
type spanEdit struct {
	s, e int
	next string
}

type textSlot struct {
	s, e   int
	kind   SlotKind
	sample string
}

func fillSlide(xml []byte, plan Plan) ([]byte, []string) {
	spans := shapeSpans(xml)
	var content, other []textSlot
	for _, sp := range spans {
		sample := shapeText(xml[sp[0]:sp[1]])
		kind := Classify(sample)
		item := textSlot{sp[0], sp[1], kind, sample}
		switch kind {
		case SlotTitle, SlotBody:
			content = append(content, item)
		case SlotSampleTitle, SlotMetaPresenter, SlotMetaDept, SlotMetaDate, SlotEnglish:
			other = append(other, item)
		}
	}
	var edits []spanEdit
	var notes []string
	point := 0
	for _, group := range pairContent(content) {
		if point >= len(plan.Points) {
			for _, item := range group {
				edits = append(edits, spanEdit{item.s, item.e, ""})
			}
			continue
		}
		p := plan.Points[point]
		point++
		body := p.Body
		if body == "" {
			body = p.Title
		}
		for _, item := range group {
			text := p.Title
			if item.kind == SlotBody {
				text = body
			}
			edits = append(edits, spanEdit{item.s, item.e, text})
		}
	}
	cover := false
	enAt := 0
	englishNoted := false
	for _, item := range other {
		switch item.kind {
		case SlotSampleTitle:
			if cover || plan.DeckTitle == "" {
				continue
			}
			cover = true
			edits = append(edits, spanEdit{item.s, item.e, plan.DeckTitle})
		case SlotMetaPresenter:
			edits = append(edits, spanEdit{item.s, item.e, metaLine("汇报人：", plan.Presenter, item.sample)})
		case SlotMetaDept:
			edits = append(edits, spanEdit{item.s, item.e, metaLine("部门：", plan.Department, item.sample)})
		case SlotMetaDate:
			edits = append(edits, spanEdit{item.s, item.e, metaLine("时间：", plan.Date, item.sample)})
		case SlotEnglish:
			en := ""
			for enAt < len(plan.Points) {
				if plan.Points[enAt].TitleEn != "" {
					en = plan.Points[enAt].TitleEn
					enAt++
					break
				}
				enAt++
			}
			if en == "" {
				if !englishNoted {
					notes = append(notes, "英文对照行未改写")
					englishNoted = true
				}
				continue
			}
			edits = append(edits, spanEdit{item.s, item.e, en})
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		ed := edits[i]
		shape := shrinkToFit(xml[ed.s:ed.e], shapeText(xml[ed.s:ed.e]), ed.next)
		shape = setShapeText(shape, ed.next)
		xml = append(xml[:ed.s], append(shape, xml[ed.e:]...)...)
	}
	return xml, notes
}

// pairContent groups a title with the adjacent body, whichever comes first.
// Designer pages put the explanation rectangle before the title box.
func pairContent(slots []textSlot) [][]textSlot {
	var groups [][]textSlot
	for i := 0; i < len(slots); {
		if i+1 < len(slots) && slots[i].kind != slots[i+1].kind {
			groups = append(groups, []textSlot{slots[i], slots[i+1]})
			i += 2
			continue
		}
		groups = append(groups, []textSlot{slots[i]})
		i++
	}
	return groups
}

func metaLine(label, value, sample string) string {
	if value != "" {
		return label + value
	}
	if strings.Contains(sample, "觅知") {
		return label
	}
	return sample
}

func shapeSpans(xml []byte) [][2]int {
	var spans [][2]int
	i := 0
	for i < len(xml) {
		j := bytes.Index(xml[i:], []byte("<p:sp"))
		if j < 0 {
			break
		}
		j += i
		if j+5 >= len(xml) {
			break
		}
		next := xml[j+5]
		if next != '>' && next != ' ' && next != '\t' && next != '\n' && next != '/' {
			i = j + 5
			continue
		}
		end := closeElement(xml, j, []byte("<p:sp"), []byte("</p:sp>"))
		if end < 0 {
			break
		}
		spans = append(spans, [2]int{j, end})
		i = end
	}
	return spans
}

func closeElement(xml []byte, open int, startTok, endTok []byte) int {
	depth := 0
	i := open
	for i < len(xml) {
		nextStart := bytes.Index(xml[i:], startTok)
		nextEnd := bytes.Index(xml[i:], endTok)
		if nextEnd < 0 {
			return -1
		}
		if nextStart >= 0 && nextStart < nextEnd {
			at := i + nextStart
			if tokenBoundary(xml, at, len(startTok)) {
				depth++
			}
			i = at + len(startTok)
			continue
		}
		at := i + nextEnd
		depth--
		i = at + len(endTok)
		if depth == 0 {
			return i
		}
	}
	return -1
}

func tokenBoundary(xml []byte, at, n int) bool {
	if at+n >= len(xml) {
		return false
	}
	c := xml[at+n]
	return c == '>' || c == ' ' || c == '\t' || c == '\n' || c == '/'
}

func shapeText(shape []byte) string {
	runs := textRuns(shape)
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(unescape(string(shape[r.start:r.end])))
	}
	return strings.TrimSpace(b.String())
}

func textRuns(shape []byte) []run {
	var runs []run
	i := 0
	for i < len(shape) {
		j := bytes.Index(shape[i:], []byte("<a:t"))
		if j < 0 {
			break
		}
		j += i
		gt := bytes.IndexByte(shape[j:], '>')
		if gt < 0 {
			break
		}
		content := j + gt + 1
		end := bytes.Index(shape[content:], []byte("</a:t>"))
		if end < 0 {
			break
		}
		end += content
		runs = append(runs, run{content, end})
		i = end + len("</a:t>")
	}
	return runs
}

func setShapeText(shape []byte, text string) []byte {
	runs := textRuns(shape)
	if len(runs) == 0 {
		return shape
	}
	esc := escape(text)
	for i := len(runs) - 1; i >= 0; i-- {
		val := ""
		if i == 0 {
			val = esc
		}
		shape = append(shape[:runs[i].start], append([]byte(val), shape[runs[i].end:]...)...)
	}
	return shape
}

func shrinkToFit(shape []byte, sample, next string) []byte {
	need := utf8.RuneCountInString(next)
	capN := shapeCapacity(shape, sample)
	if need <= capN || capN <= 0 {
		return shape
	}
	m := szAttr.FindSubmatch(shape)
	if m == nil {
		return shape
	}
	old := atoi(string(m[1]))
	if old <= 0 {
		return shape
	}
	newSz := old * capN / need
	if newSz < 1000 {
		newSz = 1000
	}
	if newSz >= old {
		return shape
	}
	from := []byte(`sz="` + itoa(old) + `"`)
	to := []byte(`sz="` + itoa(newSz) + `"`)
	return bytes.ReplaceAll(shape, from, to)
}

func shapeCapacity(shape []byte, sample string) int {
	sz, cx := 0, 0
	if m := szAttr.FindSubmatch(shape); m != nil {
		sz = atoi(string(m[1]))
	}
	if m := cxAttr.FindSubmatch(shape); m != nil {
		cx = atoi(string(m[1]))
	}
	if sz > 0 && cx > 0 {
		cw := (sz / 100) * 12700
		if cw < 1000 {
			cw = 1000
		}
		n := cx / cw
		if n < 2 {
			n = 2
		}
		if n > 80 {
			n = 80
		}
		return n
	}
	n := utf8.RuneCountInString(strings.TrimSpace(sample))
	if n < 8 {
		n = 12
	}
	return n
}

func contentSlots(xml []byte) []textSlot {
	var content []textSlot
	for _, sp := range shapeSpans(xml) {
		sample := shapeText(xml[sp[0]:sp[1]])
		kind := Classify(sample)
		if kind == SlotTitle || kind == SlotBody {
			content = append(content, textSlot{kind: kind})
		}
	}
	return content
}

func slotCounts(xml []byte) (body, title, meta int, imageOnly bool) {
	spans := shapeSpans(xml)
	textN := 0
	for _, sp := range spans {
		sample := shapeText(xml[sp[0]:sp[1]])
		if strings.TrimSpace(sample) == "" {
			continue
		}
		textN++
		switch Classify(sample) {
		case SlotBody:
			body++
		case SlotTitle:
			title++
		case SlotMetaPresenter, SlotMetaDept, SlotMetaDate:
			meta++
		}
	}
	return body, title, meta, textN == 0
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
