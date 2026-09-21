package meetings

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type notesPayload struct {
	Title         string          `json:"title"`
	Summary       string          `json:"summary"`
	Background    string          `json:"background"`
	Attendees     []string        `json:"attendees"`
	Topics        []notesTopic    `json:"topics"`
	Decisions     []string        `json:"decisions"`
	Conclusions   []string        `json:"conclusions"`
	OpenQuestions []string        `json:"openQuestions"`
	Actions       json.RawMessage `json:"actions"`
}

type notesTopic struct {
	Heading string        `json:"heading"`
	Body    string        `json:"body"`
	Points  []string      `json:"points"`
	Reasoning []string    `json:"reasoning"`
	Table   *notesTable   `json:"table"`
	Diagram *notesDiagram `json:"diagram"`
}

type notesTable struct {
	Caption string     `json:"caption"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// notesDiagram is a process the meeting described, not free-form mermaid: the
// model supplies ordered steps plus optional side branches, and we emit the
// mermaid ourselves. Letting the model write mermaid directly produces syntax
// errors that render as a red error box inside the notes.
type notesDiagram struct {
	Caption  string        `json:"caption"`
	Steps    []string      `json:"steps"`
	Branches []notesBranch `json:"branches"`
}

type notesBranch struct {
	From  string `json:"from"`
	Label string `json:"label"`
	To    string `json:"to"`
}

const reasoningHeading = "思考"

// Notes are read while the meeting is still fresh, and every extra item the
// model writes is latency the user waits through before the first card appears.
// The prompt asks for restraint; these caps make it true regardless of model.
const (
	maxTopicReasoning  = 3
	maxDiagramSteps    = 6
	maxDiagramBranches = 3
	maxDiagramLabel    = 24
)

func clampNotesPayload(p *notesPayload) {
	for i := range p.Topics {
		topic := &p.Topics[i]
		if len(topic.Reasoning) > maxTopicReasoning {
			topic.Reasoning = topic.Reasoning[:maxTopicReasoning]
		}
		if topic.Diagram == nil {
			continue
		}
		d := topic.Diagram
		if len(d.Steps) > maxDiagramSteps {
			d.Steps = d.Steps[:maxDiagramSteps]
		}
		if len(d.Branches) > maxDiagramBranches {
			d.Branches = d.Branches[:maxDiagramBranches]
		}
		for j, step := range d.Steps {
			d.Steps[j] = truncateRunes(step, maxDiagramLabel)
		}
		for j := range d.Branches {
			// From has to be truncated the same way Steps were, or a clamped step
			// no longer matches its branch origin and the flow sprouts a second,
			// disconnected copy of that box.
			d.Branches[j].From = truncateRunes(d.Branches[j].From, maxDiagramLabel)
			d.Branches[j].Label = truncateRunes(d.Branches[j].Label, maxDiagramLabel)
			d.Branches[j].To = truncateRunes(d.Branches[j].To, maxDiagramLabel)
		}
	}
}

// truncateRunes keeps a diagram box readable. A paragraph in a node label makes
// mermaid lay out one enormous box and the flow stops communicating anything.
func truncateRunes(s string, limit int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// mermaidFlowchart renders steps as a left-to-right chain and branches as
// labelled edges off it. Node ids are positional so a label repeated in two
// places still collapses onto one box, which is what a reader expects.
func mermaidFlowchart(d notesDiagram) string {
	ids := make(map[string]string)
	var order []string
	idFor := func(label string) (string, bool) {
		label = flattenDiagramLabel(label)
		if label == "" {
			return "", false
		}
		if id, ok := ids[label]; ok {
			return id, true
		}
		id := "n" + strconv.Itoa(len(order)+1)
		ids[label] = id
		order = append(order, label)
		return id, true
	}
	var chain []string
	for _, step := range d.Steps {
		if id, ok := idFor(step); ok {
			chain = append(chain, id)
		}
	}
	if len(chain) == 0 {
		return ""
	}
	type edge struct{ from, label, to string }
	var edges []edge
	for i := 1; i < len(chain); i++ {
		if chain[i-1] != chain[i] {
			edges = append(edges, edge{from: chain[i-1], to: chain[i]})
		}
	}
	for _, branch := range d.Branches {
		from, okFrom := idFor(branch.From)
		to, okTo := idFor(branch.To)
		if !okFrom || !okTo || from == to {
			continue
		}
		edges = append(edges, edge{from: from, label: flattenDiagramLabel(branch.Label), to: to})
	}
	if len(edges) == 0 {
		// One box is not a process. Drawing it anyway turns a stray step into a
		// diagram that says nothing.
		return ""
	}
	// Declare every node first (branches may have added some), then the edges.
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, label := range order {
		b.WriteString("  " + ids[label] + `["` + label + "\"]\n")
	}
	for _, e := range edges {
		b.WriteString("  " + e.from + " -->")
		if e.label != "" {
			b.WriteString(`|"` + e.label + `"|`)
		}
		b.WriteString(" " + e.to + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// flattenDiagramLabel keeps a label on one line and out of mermaid's way. Double
// quotes close the label early and brackets start a new shape, so both go.
func flattenDiagramLabel(label string) string {
	label = strings.TrimSpace(label)
	label = strings.NewReplacer("\n", " ", "\r", " ", `"`, "'", "[", "(", "]", ")", "|", "/").Replace(label)
	return strings.TrimSpace(strings.Join(strings.Fields(label), " "))
}

func parseJSONNotes(raw string) (Notes, bool) {
	trimmed := strings.TrimSpace(raw)
	if i := strings.Index(trimmed, "```"); i >= 0 {
		rest := trimmed[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
		rest = strings.TrimPrefix(rest, "\n")
		if end := strings.Index(rest, "```"); end >= 0 {
			trimmed = strings.TrimSpace(rest[:end])
		}
	}
	if !strings.HasPrefix(trimmed, "{") {
		if start := strings.Index(trimmed, "{"); start >= 0 {
			if end := strings.LastIndex(trimmed, "}"); end > start {
				trimmed = trimmed[start : end+1]
			}
		}
	}
	var payload notesPayload
	if json.Unmarshal([]byte(trimmed), &payload) != nil {
		cleaned := cleanLLMJSON(trimmed)
		if json.Unmarshal([]byte(cleaned), &payload) != nil {
			return Notes{}, false
		}
	}
	clampNotesPayload(&payload)
	summary := composeStructuredSummary(payload)
	if summary == "" {
		summary = strings.TrimSpace(payload.Summary)
	}
	actions := decodeActions(payload.Actions)
	if summary == "" && actions == "" {
		return Notes{}, false
	}
	return Notes{Title: strings.TrimSpace(payload.Title), Summary: summary, Actions: actions}, true
}

func cleanLLMJSON(raw string) string {
	out := raw
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\t", " ")
	out = strings.ReplaceAll(out, "\xEF\xBB\xBF", "")
	out = controlCharRe.ReplaceAllStringFunc(out, func(m string) string {
		if m == "\n" {
			return m
		}
		return " "
	})
	lines := strings.Split(out, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	out = strings.Join(cleaned, "\n")
	for {
		next := trailingCommaRe.ReplaceAllString(out, "$1")
		if next == out {
			break
		}
		out = next
	}
	out = unescapedNewlineInStringRe.ReplaceAllString(out, `$1\n$2`)
	return out
}

var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)
var controlCharRe = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`)
var unescapedNewlineInStringRe = regexp.MustCompile(`("(?:[^"\\]|\\.)*)` + "\n" + `((?:[^"\\]|\\.)*")`)

func composeStructuredSummary(payload notesPayload) string {
	var b strings.Builder
	if names := joinNoteNames(payload.Attendees); names != "" {
		b.WriteString("参会：")
		b.WriteString(names)
		b.WriteString("\n\n")
	}
	if bg := strings.TrimSpace(payload.Background); bg != "" {
		b.WriteString("## 背景\n\n")
		b.WriteString(bg)
		b.WriteString("\n\n")
	}
	for _, topic := range payload.Topics {
		heading := strings.TrimSpace(topic.Heading)
		if heading == "" {
			heading = "讨论要点"
		}
		b.WriteString("## 议题：")
		b.WriteString(heading)
		b.WriteString("\n\n")
		if body := strings.TrimSpace(topic.Body); body != "" {
			b.WriteString(body)
			b.WriteString("\n\n")
		}
		wrotePoints := false
		for _, point := range topic.Points {
			point = strings.TrimSpace(point)
			if point == "" {
				continue
			}
			if !strings.HasPrefix(point, "- ") && !strings.HasPrefix(point, "-") {
				point = "- " + point
			} else if strings.HasPrefix(point, "-") && !strings.HasPrefix(point, "- ") {
				point = "- " + strings.TrimSpace(strings.TrimPrefix(point, "-"))
			}
			b.WriteString(point)
			b.WriteByte('\n')
			wrotePoints = true
		}
		if wrotePoints {
			b.WriteByte('\n')
		}
		// Order matters and mirrors how the notes are meant to be read: what the
		// conversation said, then what we make of it, then the table or flow that
		// the reasoning called for. A table or diagram placed before the reasoning
		// reads like decoration.
		writeNoteReasoning(&b, topic.Reasoning)
		if topic.Table != nil && len(topic.Table.Headers) > 0 {
			writeMarkdownTable(&b, *topic.Table)
			b.WriteByte('\n')
		}
		writeNoteDiagram(&b, topic.Diagram)
	}
	decisions := payload.Decisions
	if len(decisions) == 0 {
		decisions = payload.Conclusions
	}
	writeNoteList(&b, "决议", decisions)
	writeNoteList(&b, "未决", payload.OpenQuestions)
	return strings.TrimSpace(b.String())
}

// writeNoteReasoning emits the thinking record under its own subheading so the
// renderer can style it apart from the transcript facts above it.
func writeNoteReasoning(b *strings.Builder, reasoning []string) {
	var kept []string
	for _, line := range reasoning {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return
	}
	b.WriteString("### " + reasoningHeading + "\n\n")
	for _, line := range kept {
		b.WriteString(normalizeNoteBullet(line))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
}

// writeNoteDiagram emits a mermaid fence. The chat renderer already draws these,
// and the HTML export turns the same fence into a static box chain, so one
// carrier serves both without shipping a script into the exported file.
func writeNoteDiagram(b *strings.Builder, diagram *notesDiagram) {
	if diagram == nil {
		return
	}
	code := mermaidFlowchart(*diagram)
	if code == "" {
		return
	}
	if caption := flattenDiagramLabel(diagram.Caption); caption != "" {
		b.WriteString("### " + caption + "\n\n")
	}
	b.WriteString("```mermaid\n")
	b.WriteString(code)
	b.WriteString("\n```\n\n")
}

func normalizeNoteBullet(item string) string {
	item = strings.TrimSpace(item)
	if item == "" {
		return item
	}
	if strings.HasPrefix(item, "- ") {
		return item
	}
	if strings.HasPrefix(item, "-") {
		return "- " + strings.TrimSpace(strings.TrimPrefix(item, "-"))
	}
	return "- " + item
}

func writeNoteList(b *strings.Builder, heading string, items []string) {
	wrote := false
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !wrote {
			b.WriteString("## ")
			b.WriteString(heading)
			b.WriteString("\n\n")
			wrote = true
		}
		item = normalizeNoteBullet(item)
		b.WriteString(item)
		b.WriteByte('\n')
	}
	if wrote {
		b.WriteByte('\n')
	}
}

func writeMarkdownTable(b *strings.Builder, table notesTable) {
	if caption := strings.TrimSpace(table.Caption); caption != "" {
		b.WriteString("### ")
		b.WriteString(caption)
		b.WriteString("\n\n")
	}
	b.WriteString("|")
	for _, header := range table.Headers {
		b.WriteString(" ")
		b.WriteString(escapePipe(header))
		b.WriteString(" |")
	}
	b.WriteByte('\n')
	b.WriteString("|")
	for range table.Headers {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')
	for _, row := range table.Rows {
		b.WriteString("|")
		for i := range table.Headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(" ")
			b.WriteString(escapePipe(cell))
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}
}

func escapePipe(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "|", "\\|")
}

func joinNoteNames(names []string) string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return strings.Join(out, "、")
}

func decodeActions(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var items []string
	if json.Unmarshal(raw, &items) == nil {
		return joinActionLines(items)
	}
	var objects []map[string]any
	if json.Unmarshal(raw, &objects) == nil {
		lines := make([]string, 0, len(objects))
		for _, object := range objects {
			if line := formatActionObject(object); line != "" {
				lines = append(lines, line)
			}
		}
		return joinActionLines(lines)
	}
	return strings.TrimSpace(string(raw))
}

func formatActionObject(object map[string]any) string {
	task := firstNonEmpty(
		stringField(object, "task"),
		stringField(object, "text"),
		stringField(object, "action"),
	)
	owner := firstNonEmpty(stringField(object, "owner"), stringField(object, "who"))
	due := firstNonEmpty(stringField(object, "due"), stringField(object, "deadline"))
	if task == "" {
		return ""
	}
	if owner != "" {
		task = owner + "：" + task
	}
	if due != "" {
		task += "（截止：" + due + "）"
	}
	return task
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinActionLines(items []string) string {
	var b strings.Builder
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = normalizeNoteBullet(item)
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item)
	}
	return b.String()
}

func sectionBetween(raw string, starts, ends []string) string {
	lower := strings.ToLower(raw)
	startAt := -1
	for _, label := range starts {
		idx := indexHeading(lower, strings.ToLower(label))
		if idx >= 0 && (startAt < 0 || idx < startAt) {
			startAt = idx
		}
	}
	if startAt < 0 {
		return ""
	}
	from := skipHeadingLine(raw, startAt)
	endAt := len(raw)
	for _, label := range ends {
		idx := indexHeading(lower[from:], strings.ToLower(label))
		if idx >= 0 {
			abs := from + idx
			if abs < endAt {
				endAt = abs
			}
		}
	}
	return strings.TrimSpace(raw[from:endAt])
}

func indexHeading(lower, label string) int {
	patterns := []string{"## " + label, "# " + label, label + "\n", label + "：", label + ":"}
	best := -1
	for _, p := range patterns {
		if i := strings.Index(lower, p); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

func skipHeadingLine(raw string, at int) int {
	rest := raw[at:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		return at + i + 1
	}
	return at
}

func notesExportHTML(title, summary, actions, transcript, meta string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title><style>`)
	b.WriteString(notesExportCSS)
	b.WriteString(`</style></head><body><article class="notes-doc">`)
	b.WriteString(`<header class="notes-doc-hero"><h1>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</h1>`)
	if strings.TrimSpace(meta) != "" {
		b.WriteString(`<p class="notes-doc-meta">`)
		b.WriteString(html.EscapeString(meta))
		b.WriteString(`</p>`)
	}
	b.WriteString(`</header>`)
	writeNotesHTMLFromMarkdown(&b, summary)
	if strings.TrimSpace(actions) != "" {
		b.WriteString(`<section class="notes-doc-card notes-doc-todos"><h2>决议 / 待办</h2>`)
		writeNotesHTMLList(&b, actions)
		b.WriteString(`</section>`)
	}
	body := strings.TrimSpace(transcript)
	if body == "" {
		body = "（空）"
	}
	b.WriteString(`<section class="notes-doc-card"><h2>全文逐字稿</h2><p>`)
	b.WriteString(html.EscapeString(body))
	b.WriteString(`</p></section></article></body></html>`)
	return b.String()
}

const notesExportCSS = `body{margin:0;background:#f4f5f7;color:#1f2329;font:15px/1.7 "Segoe UI","PingFang SC","Microsoft YaHei UI",sans-serif}
.notes-doc{max-width:860px;margin:0 auto;padding:32px 20px 48px;display:grid;gap:14px}
.notes-doc-hero h1{margin:0 0 8px;font-size:26px}
.notes-doc-meta{margin:0;color:#646a73;font-size:13px}
.notes-doc-card{background:#fff;border:1px solid #dee0e3;border-radius:12px;padding:20px 22px}
.notes-doc-card h2{margin:0 0 12px;font-size:16px}
.notes-doc-card p{margin:0 0 10px}
.notes-doc-card ul,.notes-doc-card ol{margin:0;padding-left:1.2em;display:grid;gap:8px}
.notes-doc-table{width:100%;border-collapse:collapse;margin-top:10px;font-size:13px}
.notes-doc-table th,.notes-doc-table td{border:1px solid #dee0e3;padding:8px 10px;text-align:left;vertical-align:top}
.notes-doc-table th{background:#f5f6f7;color:#646a73}
.notes-doc-subhead{margin:14px 0 6px;color:#646a73;font-size:13px;font-weight:600}
.notes-doc-reasoning{margin-top:12px;padding:12px 14px;border-left:3px solid #b8c4d9;border-radius:0 8px 8px 0;background:#f7f9fc}
.notes-doc-reasoning .notes-doc-subhead{margin-top:0}
.notes-doc-flow{display:flex;flex-wrap:wrap;align-items:center;gap:8px 6px;margin-top:8px}
.notes-doc-flow-node{padding:7px 12px;border:1px solid #c9d2e3;border-radius:8px;background:#fff;font-size:13px;white-space:nowrap}
.notes-doc-flow-arrow{color:#8a9099;font-size:12px;white-space:nowrap}
.notes-doc-flow-break{flex-basis:100%;height:0}
.notes-doc-todos{border-left:3px solid #3370ff}`

func writeNotesHTMLFromMarkdown(b *strings.Builder, raw string) {
	text := strings.TrimSpace(raw)
	if text == "" {
		b.WriteString(`<section class="notes-doc-card"><p>尚未生成摘要。</p></section>`)
		return
	}
	if strings.HasPrefix(text, "参会：") || strings.HasPrefix(text, "参会:") {
		line, rest, _ := strings.Cut(text, "\n")
		names := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "参会："), "参会:"))
		if names != "" {
			b.WriteString(`<section class="notes-doc-card"><h2>参会人</h2><p>`)
			b.WriteString(html.EscapeString(names))
			b.WriteString(`</p></section>`)
		}
		text = strings.TrimSpace(rest)
	}
	chunks := splitNoteHeadings(text)
	if len(chunks) == 0 {
		b.WriteString(`<section class="notes-doc-card">`)
		writeNotesHTMLBlocks(b, text)
		b.WriteString(`</section>`)
		return
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.body) == "" && strings.TrimSpace(chunk.heading) == "" {
			continue
		}
		b.WriteString(`<section class="notes-doc-card"><h2>`)
		b.WriteString(html.EscapeString(chunk.heading))
		b.WriteString(`</h2>`)
		writeNotesHTMLBlocks(b, chunk.body)
		b.WriteString(`</section>`)
	}
}

type noteChunk struct {
	heading string
	body    string
}

func splitNoteHeadings(raw string) []noteChunk {
	lines := strings.Split(raw, "\n")
	var chunks []noteChunk
	current := noteChunk{heading: "会议摘要"}
	started := false
	flush := func() {
		current.body = strings.TrimSpace(current.body)
		if current.body == "" && !started {
			return
		}
		chunks = append(chunks, current)
	}
	for _, line := range lines {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			if started || strings.TrimSpace(current.body) != "" {
				flush()
			}
			current = noteChunk{heading: strings.TrimSpace(heading)}
			started = true
			continue
		}
		current.body += line + "\n"
	}
	if started || strings.TrimSpace(current.body) != "" {
		flush()
	}
	return chunks
}

func writeNotesHTMLBlocks(b *strings.Builder, raw string) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var para []string
	var bullets []string
	var table []string
	flushPara := func() {
		text := strings.TrimSpace(strings.Join(para, "\n"))
		para = para[:0]
		if text == "" {
			return
		}
		b.WriteString(`<p>`)
		b.WriteString(html.EscapeString(text))
		b.WriteString(`</p>`)
	}
	flushBullets := func() {
		if len(bullets) == 0 {
			return
		}
		b.WriteString(`<ul>`)
		for _, item := range bullets {
			b.WriteString(`<li>`)
			b.WriteString(html.EscapeString(item))
			b.WriteString(`</li>`)
		}
		b.WriteString(`</ul>`)
		bullets = bullets[:0]
	}
	flushTable := func() {
		if len(table) < 2 {
			para = append(para, table...)
			table = table[:0]
			return
		}
		headers := splitPipeRow(table[0])
		rows := table[1:]
		if len(rows) > 0 && isAlignRow(rows[0]) {
			rows = rows[1:]
		}
		b.WriteString(`<table class="notes-doc-table"><thead><tr>`)
		for _, header := range headers {
			b.WriteString(`<th>`)
			b.WriteString(html.EscapeString(header))
			b.WriteString(`</th>`)
		}
		b.WriteString(`</tr></thead><tbody>`)
		for _, row := range rows {
			b.WriteString(`<tr>`)
			cells := splitPipeRow(row)
			for i := range headers {
				cell := ""
				if i < len(cells) {
					cell = cells[i]
				}
				b.WriteString(`<td>`)
				b.WriteString(html.EscapeString(cell))
				b.WriteString(`</td>`)
			}
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
		table = table[:0]
	}
	var fence []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				// The exported file carries no scripts, so the flow is drawn with
				// boxes and arrows instead of handing mermaid source to a reader.
				writeNotesHTMLFlow(b, fence)
				fence, inFence = nil, false
				continue
			}
			flushTable()
			flushBullets()
			flushPara()
			inFence = true
			continue
		}
		if inFence {
			fence = append(fence, trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			flushTable()
			flushBullets()
			flushPara()
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			b.WriteString(`<p class="notes-doc-subhead">`)
			b.WriteString(html.EscapeString(heading))
			b.WriteString(`</p>`)
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			flushBullets()
			flushPara()
			table = append(table, trimmed)
			continue
		}
		if len(table) > 0 {
			flushTable()
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-") && len(trimmed) > 1 {
			flushPara()
			bullets = append(bullets, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "-")))
			continue
		}
		if trimmed == "" {
			flushTable()
			flushBullets()
			flushPara()
			continue
		}
		flushBullets()
		para = append(para, trimmed)
	}
	if inFence {
		writeNotesHTMLFlow(b, fence)
	}
	flushTable()
	flushBullets()
	flushPara()
}

var mermaidNodeRe = regexp.MustCompile(`^(\w+)\["(.*)"\]$`)
var mermaidEdgeRe = regexp.MustCompile(`^(\w+)\s*-->\s*(?:\|"([^"]*)"\|\s*)?(\w+)$`)

// writeNotesHTMLFlow turns the mermaid we generated in writeNoteDiagram back
// into inert HTML. It only understands our own emitted shape; anything else is
// dropped rather than dumped as source, because raw mermaid in an exported
// document reads as a broken artifact.
func writeNotesHTMLFlow(b *strings.Builder, fence []string) {
	labels := map[string]string{}
	type edge struct{ from, label, to string }
	var edges []edge
	for _, line := range fence {
		if node := mermaidNodeRe.FindStringSubmatch(line); node != nil {
			labels[node[1]] = node[2]
			continue
		}
		if e := mermaidEdgeRe.FindStringSubmatch(line); e != nil {
			edges = append(edges, edge{from: e[1], label: e[2], to: e[3]})
		}
	}
	if len(edges) == 0 {
		return
	}
	// Walk the edges in order so the reader sees the same path the notes claim,
	// repeating a node label when a branch re-enters it.
	b.WriteString(`<div class="notes-doc-flow">`)
	for i, e := range edges {
		if i == 0 || edges[i-1].to != e.from {
			if i > 0 {
				b.WriteString(`<span class="notes-doc-flow-break"></span>`)
			}
			writeFlowNode(b, labels[e.from], e.from)
		}
		b.WriteString(`<span class="notes-doc-flow-arrow">`)
		if e.label != "" {
			b.WriteString(html.EscapeString(e.label))
		}
		b.WriteString(`</span>`)
		writeFlowNode(b, labels[e.to], e.to)
	}
	b.WriteString(`</div>`)
}

func writeFlowNode(b *strings.Builder, label, fallback string) {
	if label == "" {
		label = fallback
	}
	b.WriteString(`<span class="notes-doc-flow-node">`)
	b.WriteString(html.EscapeString(label))
	b.WriteString(`</span>`)
}

func writeNotesHTMLList(b *strings.Builder, raw string) {
	b.WriteString(`<ul>`)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "□ "), "* "))
		if line == "" {
			continue
		}
		b.WriteString(`<li>`)
		b.WriteString(html.EscapeString(line))
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
}

func splitPipeRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(strings.ReplaceAll(part, "\\|", "|")))
	}
	return out
}

func isAlignRow(line string) bool {
	for _, cell := range splitPipeRow(line) {
		if strings.Trim(cell, " :-") != "" {
			return false
		}
	}
	return true
}
