package app

import (
	"encoding/csv"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/officetools"
)

var officeChapterHeading = regexp.MustCompile(`^第[一二三四五六七八九十百零〇0-9]+[章节篇部卷].{0,100}$`)
var officeTableRule = regexp.MustCompile(`^:?-{3,}:?$`)
var officeTableNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,13})(?:\.[0-9]+)?$`)

// These conversions preserve supplied content. They never invent a report,
// slide body or spreadsheet row when the model produced only a promise.
func officeContentUsable(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 512<<10 || isCompanionLeadInOnly(text) {
		return false
	}
	return strings.Contains(text, "\n") || utf8.RuneCountInString(text) > 80 || !looksLikeCompanionWaitPromise(text)
}

func officeHeading(line string) (string, string) {
	if trimmed := strings.TrimLeft(line, "#"); len(trimmed) < len(line) && strings.HasPrefix(trimmed, " ") {
		kind := "heading"
		if len(line)-len(trimmed) > 1 {
			kind = "heading2"
		}
		return kind, strings.TrimSpace(trimmed)
	}
	if officeChapterHeading.MatchString(line) {
		return "heading", line
	}
	return "", line
}

func officeDocxBlocks(title, text string) []officetools.DocxBlock {
	if !officeContentUsable(text) {
		return nil
	}
	var blocks []officetools.DocxBlock
	var paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			blocks = append(blocks, officetools.DocxBlock{Type: "paragraph", Text: strings.Join(paragraph, "\n")})
			paragraph = nil
		}
	}
	heading := false
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		kind, content := officeHeading(line)
		switch {
		case line == "":
			flush()
		case kind != "":
			flush()
			blocks = append(blocks, officetools.DocxBlock{Type: kind, Text: content})
			heading = true
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			flush()
			blocks = append(blocks, officetools.DocxBlock{Type: "bullet", Text: line[2:]})
		case strings.HasPrefix(line, "> "):
			flush()
			blocks = append(blocks, officetools.DocxBlock{Type: "quote", Text: line[2:]})
		default:
			paragraph = append(paragraph, raw)
		}
	}
	flush()
	if !heading {
		blocks = append([]officetools.DocxBlock{{Type: "heading", Text: title}}, blocks...)
	}
	if len(blocks) > officetools.MaxDocxBlocks {
		return nil
	}
	return blocks
}

func officeContentSlides(goal, text string) []map[string]any {
	if !officeContentUsable(text) {
		return nil
	}
	var slides []map[string]any
	title := clipOfficeTitle(goal, "演示文稿")
	var bullets []string
	pageRunes := 0
	pendingHeading, bodySeen := false, false
	flush := func() {
		if len(bullets) == 0 {
			return
		}
		slides = append(slides, map[string]any{"title": title, "layout": "content", "bullets": bullets})
		bullets, pageRunes = nil, 0
		pendingHeading = false
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if kind, heading := officeHeading(line); kind != "" {
			if pendingHeading && len(bullets) == 0 {
				// Keep an intentionally heading-only section instead of dropping it.
				slides = append(slides, map[string]any{"title": title, "layout": "section"})
			}
			flush()
			title = heading
			pendingHeading = true
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = line[2:]
		}
		// Split long paragraphs into readable pages without losing the tail.
		chars := []rune(line)
		bodySeen = bodySeen || len(chars) > 0
		for len(chars) > 0 {
			n := min(len(chars), 100)
			if len(bullets) >= 6 || pageRunes+n > 480 {
				flush()
			}
			bullets = append(bullets, string(chars[:n]))
			pageRunes += n
			chars = chars[n:]
		}
	}
	flush()
	if pendingHeading {
		slides = append(slides, map[string]any{"title": title, "layout": "section"})
	}
	if !bodySeen || len(slides) == 0 || len(slides) > officetools.MaxPptxSlides || utf8.RuneCountInString(text) < 40 {
		return nil
	}
	return slides
}

func officeCellValue(cell string) any {
	// Keep leading-zero identifiers and long account numbers as exact text.
	if officeTableNumber.MatchString(cell) {
		if value, err := strconv.ParseFloat(cell, 64); err == nil {
			return value
		}
	}
	return cell
}

func officeTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	// Escaped pipes are literal cell text, not additional columns.
	var cells []string
	var b strings.Builder
	escaped := false
	for _, r := range line {
		if escaped {
			if r != '|' && r != '\\' {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
			escaped = false
		} else if r == '\\' {
			escaped = true
		} else if r == '|' {
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		} else {
			b.WriteRune(r)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	return append(cells, strings.TrimSpace(b.String()))
}

func officeContentSheets(text string) []officetools.SheetSpec {
	if !officeContentUsable(text) {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var sheets []officetools.SheetSpec
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```csv" {
			start := i + 1
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "```"; i++ {
			}
			if i == len(lines) {
				return nil
			}
			sheet := officeCSVSheet(strings.Join(lines[start:i], "\n"))
			if sheet == nil {
				return nil
			}
			sheets = append(sheets, *sheet)
			continue
		}
		if i+1 == len(lines) {
			break
		}
		if !strings.Contains(lines[i], "|") {
			continue
		}
		headers, rule := officeTableCells(lines[i]), officeTableCells(lines[i+1])
		valid := len(headers) == len(rule) && len(headers) > 1
		for _, cell := range rule {
			valid = valid && officeTableRule.MatchString(cell)
		}
		if !valid {
			continue
		}
		sheet := officetools.SheetSpec{Headers: headers}
		i += 2
		for ; i < len(lines) && strings.Contains(lines[i], "|"); i++ {
			cells := officeTableCells(lines[i])
			if len(cells) != len(headers) {
				return nil // malformed input must not silently drop a column
			}
			row := make([]any, len(cells))
			for j, cell := range cells {
				row[j] = officeCellValue(cell)
			}
			sheet.Rows = append(sheet.Rows, row)
		}
		i--
		if len(sheet.Rows) == 0 {
			return nil
		}
		sheets = append(sheets, sheet)
	}
	return sheets
}

// Only explicitly fenced CSV is unambiguous; prose is never a fake table.
func officeCSVSheet(body string) *officetools.SheetSpec {
	r := csv.NewReader(strings.NewReader(body))
	headers, err := r.Read()
	if err != nil || len(headers) < 2 {
		return nil
	}
	sheet := officetools.SheetSpec{Headers: headers}
	for {
		cells, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		row := make([]any, len(cells))
		for i, cell := range cells {
			row[i] = officeCellValue(cell)
		}
		sheet.Rows = append(sheet.Rows, row)
	}
	if len(sheet.Rows) == 0 {
		return nil
	}
	return &sheet
}
