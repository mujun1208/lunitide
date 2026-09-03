// Package doctext extracts a plain-text body from the office and PDF formats
// the KB ingest path accepts. Every extractor is pure Go (CGO_ENABLED=0 safe):
// DOCX/PPTX are OOXML zip+XML parsed with the stdlib, XLSX rides the already
// vendored excelize reader, and PDF uses the pure-Go ledongthuc/pdf text
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
	"github.com/xuri/excelize/v2"
)

// MaxExtractRunes caps any single extraction so a pathological file cannot
// exhaust memory before the KB chunker applies its own per-version caps.
const MaxExtractRunes = 2_000_000

// maxBuildBytes bounds the transient builder while decoding: extractors stop
// appending well before capRunes trims the result, so a huge (or malicious)
// office/PDF file cannot balloon memory during extraction.
const maxBuildBytes = MaxExtractRunes * 4

var (
	// ErrUnsupportedFormat is returned for a binary input doctext cannot turn
	// into text (an image, an archive, an unknown proprietary container).
	ErrUnsupportedFormat = errors.New("doctext: unsupported binary format")
	// ErrNoTextLayer is returned when a recognised container held no
	// extractable text — most often a scanned / image-only PDF.
	ErrNoTextLayer = errors.New("doctext: no extractable text layer")
)

// Result is one extraction outcome.
type Result struct {
	Text  string // extracted plain text (trimmed, rune-capped)
	Media string // media type to drive KB splitting: text/markdown or text/plain
	Kind  string // detected source kind: markdown|plain|docx|pptx|xlsx|pdf
}

// Extract turns a local file's bytes into a searchable plain-text body. The
// declared media hint is only a tie-breaker: extension and magic bytes win so
// a mislabelled upload still routes to the right parser.
func Extract(path string, raw []byte, declaredMedia string) (Result, error) {
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
	default:
		return Result{}, ErrUnsupportedFormat
	}
}

func textResult(text, media, kind string) (Result, error) {
	text = capRunes(strings.TrimSpace(text), MaxExtractRunes)
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
	case ".pptx":
		return "pptx"
	case ".xlsx":
		return "xlsx"
	case ".pdf":
		return "pdf"
	}
	m := strings.ToLower(strings.TrimSpace(declaredMedia))
	switch {
	case strings.Contains(m, "wordprocessingml"):
		return "docx"
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
			break
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
			break
		}
	}
	return b.String(), nil
}

func xlsxText(raw []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("doctext: xlsx: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			line := strings.TrimSpace(strings.Join(row, "\t"))
			if line == "" {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
			if b.Len() >= maxBuildBytes {
				return b.String(), nil
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
			err = fmt.Errorf("%w: %v", ErrNoTextLayer, r)
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("doctext: pdf: %w", err)
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoTextLayer, err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(plain, int64(MaxExtractRunes)*4)); err != nil {
		return "", fmt.Errorf("doctext: pdf read: %w", err)
	}
	return buf.String(), nil
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
		data, err := io.ReadAll(io.LimitReader(rc, max))
		if err != nil {
			return "", err
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

func capRunes(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
