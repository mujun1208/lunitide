// Package doctext extracts a plain-text body from the office and PDF formats
// the KB ingest path accepts. Every extractor is pure Go (CGO_ENABLED=0 safe):
// DOCX/PPTX/XLSX are OOXML zip+XML parsed with the stdlib, and PDF uses the
// pure-Go ledongthuc/pdf text
// layer. A binary input with no recoverable text fails closed so a manual is
// never ingested as garbage bytes.
package doctext

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/lunitide/lunitide/internal/xlsxrows"
)

// MaxExtractRunes caps any single extraction so a pathological file cannot
// exhaust memory before the KB chunker applies its own per-version caps.
const MaxExtractRunes = 2_000_000
const MaxInputBytes = 32 << 20

// maxBuildBytes bounds the transient builder while decoding: extractors stop
// appending at the configured limit, so a huge (or malicious)
// office/PDF file cannot balloon memory during extraction.
const maxBuildBytes = MaxExtractRunes * 4

var (
	// ErrUnsupportedFormat is returned for a binary input doctext cannot turn
	// into text (an image, an archive, an unknown proprietary container).
	ErrBudgetExceeded    = errors.New("doctext: parsing budget exceeded")
	ErrUnsupportedFormat = errors.New("doctext: unsupported binary format")
	// ErrNoTextLayer is returned when a recognised container held no
	// extractable text — most often a scanned / image-only PDF.
	ErrNoTextLayer = errors.New("doctext: no extractable text layer")
	// ErrUnreadableTextLayer means bytes came out, but they are not Unicode
	// text. A PDF can still draw the right glyphs from an embedded font
	// while its text layer has no ToUnicode map.
	ErrUnreadableTextLayer = errors.New("doctext: text layer is not readable unicode")
)

// Result is one extraction outcome.
type Result struct {
	Text  string // complete extracted plain text (trimmed, budget-checked)
	Media string // media type to drive KB splitting: text/markdown or text/plain
	Kind  string // detected source kind: markdown|plain|docx|pptx|xlsx|pdf
}

// Extract turns a local file's bytes into a searchable plain-text body. The
// declared media hint is only a tie-breaker: extension and magic bytes win so
// a mislabelled upload still routes to the right parser.
func Extract(path string, raw []byte, declaredMedia string) (Result, error) {
	if len(raw) > MaxInputBytes {
		return Result{}, ErrBudgetExceeded
	}
	if bytes.HasPrefix(raw, []byte("PK")) {
		if err := validateArchive(raw); err != nil {
			return Result{}, err
		}
	}
	switch classify(path, declaredMedia, raw) {
	case "markdown":
		return textResult(string(raw), "text/markdown", "markdown")
	case "plain":
		return textResult(string(raw), "text/plain", "plain")
	case "docx":
		return extracted(docxText, raw, "docx")
	case "pptx":
		return extracted(pptxText, raw, "pptx")
	case "xlsx":
		return extracted(xlsxText, raw, "xlsx")
	case "pdf":
		return extracted(pdfText, raw, "pdf")
	case "rtf":
		return extracted(rtfText, raw, "rtf")
	case "odt":
		return extracted(odtText, raw, "odt")
	case "doc":
		return extracted(docText, raw, "doc")
	default:
		return Result{}, ErrUnsupportedFormat
	}
}

func textResult(text, media, kind string) (Result, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) {
		return Result{}, ErrUnsupportedFormat
	}
	if utf8.RuneCountInString(text) > MaxExtractRunes {
		return Result{}, ErrBudgetExceeded
	}
	if text == "" {
		return Result{}, ErrNoTextLayer
	}
	return Result{Text: text, Media: media, Kind: kind}, nil
}

func extracted(fn func([]byte) (string, error), raw []byte, kind string) (Result, error) {
	text, err := fn(raw)
	if err != nil {
		return Result{}, err
	}
	return textResult(text, "text/plain", kind)
}

// classify resolves the parser to use: extension first (the user's intent),
// then the declared media hint, then magic-byte sniffing for a mislabelled or
// extensionless upload.
func classify(path, declaredMedia string, raw []byte) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return "markdown"
	case ".txt", ".text", ".log", ".csv", ".tsv":
		return "plain"
	case ".docx":
		return "docx"
	case ".doc":
		return "doc"
	case ".rtf":
		return "rtf"
	case ".odt":
		return "odt"
	case ".pptx":
		return "pptx"
	case ".xlsx":
		return "xlsx"
	case ".pdf":
		return "pdf"
	case ".ppt":
		if bytes.HasPrefix(raw, []byte("PK")) {
			return "pptx"
		}
		return "doc"
	case ".xls":
		if bytes.HasPrefix(raw, []byte("PK")) {
			return "xlsx"
		}
		return "doc"
	case ".wps":
		if bytes.HasPrefix(raw, []byte("PK")) {
			return "docx"
		}
		if utf8.Valid(raw) && !bytes.ContainsRune(raw, 0) {
			return "plain"
		}
		return "doc"
	case ".et":
		if bytes.HasPrefix(raw, []byte("PK")) {
			return "xlsx"
		}
		return "doc"
	case ".dps":
		if bytes.HasPrefix(raw, []byte("PK")) {
			return "pptx"
		}
		return "doc"
	}
	m := strings.ToLower(strings.TrimSpace(declaredMedia))
	switch {
	case strings.Contains(m, "wordprocessingml"):
		return "docx"
	case m == "application/rtf" || m == "text/rtf":
		return "rtf"
	case strings.Contains(m, "opendocument.text"):
		return "odt"
	case m == "application/msword":
		return "doc"
	case strings.Contains(m, "ms-powerpoint"), strings.Contains(m, "ms-excel"):
		return "doc"
	case strings.Contains(m, "presentationml"):
		return "pptx"
	case strings.Contains(m, "spreadsheetml"):
		return "xlsx"
	case strings.HasPrefix(m, "application/pdf"):
		return "pdf"
	case m == "text/markdown":
		return "markdown"
	case strings.HasPrefix(m, "text/"):
		return "plain"
	}
	return sniff(raw)
}

func sniff(raw []byte) string {
	if bytes.HasPrefix(raw, []byte("%PDF-")) {
		return "pdf"
	}
	if bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		return ooxmlKind(raw)
	}
	if utf8.Valid(raw) && !bytes.ContainsRune(raw, 0) {
		return "plain"
	}
	return ""
}

func ooxmlKind(raw []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return "docx"
		case strings.HasPrefix(f.Name, "ppt/slides/slide"):
			return "pptx"
		case f.Name == "xl/workbook.xml":
			return "xlsx"
		}
	}
	return ""
}

var (
	reWordText  = regexp.MustCompile(`<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
	reWordPara  = regexp.MustCompile(`</w:p>`)
	reSlideText = regexp.MustCompile(`<a:t(?:\s[^>]*)?>(.*?)</a:t>`)
	reODTText   = regexp.MustCompile(`<text:(?:p|h|span)[^>]*>([^<]*)`)
)

func docxText(raw []byte) (string, error) {
	body, err := zipPart(raw, "word/document.xml", 64<<20)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, para := range reWordPara.Split(body, -1) {
		runs := reWordText.FindAllStringSubmatch(para, -1)
		if len(runs) == 0 {
			continue
		}
		for _, m := range runs {
			b.WriteString(html.UnescapeString(m[1]))
		}
		b.WriteByte('\n')
		if b.Len() >= maxBuildBytes {
			return "", ErrBudgetExceeded
		}
	}
	return b.String(), nil
}

func pptxText(raw []byte) (string, error) {
	names, err := zipNames(raw)
	if err != nil {
		return "", err
	}
	slides := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasPrefix(n, "ppt/slides/slide") && strings.HasSuffix(n, ".xml") {
			slides = append(slides, n)
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slideNum(slides[i]) < slideNum(slides[j]) })
	var b strings.Builder
	for _, n := range slides {
		body, err := zipPart(raw, n, 16<<20)
		if err != nil {
			return "", err
		}
		for _, m := range reSlideText.FindAllStringSubmatch(body, -1) {
			b.WriteString(html.UnescapeString(m[1]))
			b.WriteByte('\n')
		}
		if b.Len() >= maxBuildBytes {
			return "", ErrBudgetExceeded
		}
	}
	return b.String(), nil
}

func xlsxText(raw []byte) (string, error) {
	grids, err := xlsxrows.Grids(raw)
	if err != nil {
		return "", fmt.Errorf("doctext: xlsx: %w", err)
	}
	var b strings.Builder
	for _, grid := range grids {
		rows := grid.Rows
		for _, row := range rows {
			line := strings.TrimSpace(strings.Join(row, "\t"))
			if line == "" {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
			if b.Len() >= maxBuildBytes {
				return "", ErrBudgetExceeded
			}
		}
	}
	return b.String(), nil
}

// pdfText extracts the PDF text layer. ledongthuc/pdf can panic on malformed
// cross-reference tables, so a recover converts any panic into an honest
// no-text-layer failure rather than crashing the engine.
func pdfText(raw []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = ErrNoTextLayer
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("doctext: pdf: %w", err)
	}
	if reader.NumPage() > 500 {
		return "", ErrBudgetExceeded
	}
	var buf strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		fonts := map[string]*pdf.Font{} // resource names can refer to different fonts on each page
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, err := pdfPageText(page, fonts)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoTextLayer, err)
		}
		if buf.Len()+len(text) > maxBuildBytes {
			return "", ErrBudgetExceeded
		}
		buf.WriteString(text)
	}
	body := buf.String()
	if strings.TrimSpace(body) != "" && !PDFTextLayerReadable(body) {
		return "", ErrUnreadableTextLayer
	}
	return body, nil
}

func zipPart(raw []byte, name string, max int64) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("doctext: open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, max+1))
		if err != nil {
			return "", err
		}
		if int64(len(data)) > max {
			return "", ErrBudgetExceeded
		}
		return string(data), nil
	}
	return "", fmt.Errorf("doctext: zip part %q not found", name)
}

func zipNames(raw []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("doctext: open zip: %w", err)
	}
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out, nil
}

func slideNum(name string) int {
	base := strings.TrimSuffix(filepath.Base(name), ".xml")
	base = strings.TrimPrefix(base, "slide")
	n, _ := strconv.Atoi(base)
	return n
}

func rtfText(raw []byte) (string, error) {
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, "{\\rtf") {
		return "", ErrUnsupportedFormat
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '{' || c == '}' {
			i++
			continue
		}
		if c != '\\' {
			if c != '\r' && c != '\n' {
				b.WriteByte(c)
			}
			i++
			if b.Len() >= maxBuildBytes {
				return "", ErrBudgetExceeded
			}
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '\\', '{', '}':
			b.WriteByte(s[i])
			i++
		case '\'':
			if i+2 >= len(s) {
				return "", ErrUnsupportedFormat
			}
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", ErrUnsupportedFormat
			}
			if n >= 0x20 || n == '\t' {
				b.WriteByte(byte(n))
			}
			i += 3
		case 'u':
			i++
			sign := 1
			if i < len(s) && s[i] == '-' {
				sign = -1
				i++
			}
			start := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if start == i {
				continue
			}
			n, err := strconv.Atoi(s[start:i])
			if err != nil {
				return "", ErrUnsupportedFormat
			}
			n *= sign
			if n < 0 {
				n += 65536
			}
			if n > 0 && n <= 0x10FFFF {
				b.WriteRune(rune(n))
			}
			if i < len(s) && (s[i] == '?' || s[i] == ' ') {
				i++
			}
		default:
			if s[i] == '\n' || s[i] == '\r' {
				i++
				continue
			}
			start := i
			for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
				i++
			}
			word := s[start:i]
			for i < len(s) && (s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
				i++
			}
			if i < len(s) && s[i] == ' ' {
				i++
			}
			if word == "par" || word == "line" {
				b.WriteByte('\n')
			}
		}
	}
	return b.String(), nil
}

func odtText(raw []byte) (string, error) {
	body, err := zipPart(raw, "content.xml", 16<<20)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, para := range strings.Split(body, "</text:p>") {
		var line strings.Builder
		for _, m := range reODTText.FindAllStringSubmatch(para, -1) {
			line.WriteString(html.UnescapeString(m[1]))
		}
		text := strings.TrimSpace(line.String())
		if text == "" {
			continue
		}
		b.WriteString(text)
		b.WriteByte('\n')
		if b.Len() >= maxBuildBytes {
			return "", ErrBudgetExceeded
		}
	}
	return b.String(), nil
}

func docText(raw []byte) (string, error) {
	var b strings.Builder
	var run []rune
	flush := func() {
		if len(run) < 6 {
			run = run[:0]
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		for _, r := range run {
			b.WriteRune(r)
		}
		run = run[:0]
	}
	for i := 0; i+1 < len(raw); i += 2 {
		u := rune(raw[i]) | rune(raw[i+1])<<8
		if u == '\r' || u == '\n' || u == 0x07 || u == 0x0b {
			flush()
			continue
		}
		if u == '\t' || u == ' ' || (u >= 0x21 && u < 0xD800) || (u >= 0xE000 && u <= 0xFFFD) {
			run = append(run, u)
			if len(run) > 4000 {
				flush()
			}
			continue
		}
		flush()
		if b.Len() >= maxBuildBytes {
			return "", ErrBudgetExceeded
		}
	}
	flush()
	return b.String(), nil
}

func validateArchive(raw []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	if len(archive.File) > 2048 {
		return ErrBudgetExceeded
	}
	var total uint64
	for _, part := range archive.File {
		if part.UncompressedSize64 > 16<<20 {
			return ErrBudgetExceeded
		}
		total += part.UncompressedSize64
		if total > 64<<20 {
			return ErrBudgetExceeded
		}
	}
	return nil
}
