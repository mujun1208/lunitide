package meetings

import (
	"encoding/json"
	"html"
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
	Heading string      `json:"heading"`
	Body    string      `json:"body"`
	Points  []string    `json:"points"`
	Table   *notesTable `json:"table"`
}

type notesTable struct {
	Caption string     `json:"caption"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

func parseJSONNotes(raw string) (Notes, bool) {
	trimmed := strings.TrimSpace(raw)
	if i := strings.Index(trimmed, "```"); i >= 0 {
		rest := trimmed[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		rest = strings.TrimPrefix(rest, "JSON")
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
		return Notes{}, false
	}
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
		if topic.Table != nil && len(topic.Table.Headers) > 0 {
			writeMarkdownTable(&b, *topic.Table)
			b.WriteByte('\n')
		}
	}
	decisions := payload.Decisions
	if len(decisions) == 0 {
		decisions = payload.Conclusions
	}
	writeNoteList(&b, "决议", decisions)
	writeNoteList(&b, "未决", payload.OpenQuestions)
	return strings.TrimSpace(b.String())
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
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flushTable()
			flushBullets()
			flushPara()
			b.WriteString(`<p>`)
			b.WriteString(html.EscapeString(strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))))
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
	flushTable()
	flushBullets()
	flushPara()
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
