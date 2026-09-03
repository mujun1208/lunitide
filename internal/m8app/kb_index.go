package m8app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const parseChunkRunes = 1200

// ParseBodyIndexer reads a local file and splits it into chunks with body.
// Markdown/plain split on ATX headings or every 1200 runes. Empty files fail.
func ParseBodyIndexer(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
	_ = ctx
	ref := strings.TrimSpace(doc.ContentRef)
	if ref == "" || !filepath.IsAbs(ref) {
		return nil, fmt.Errorf("content_ref must be an absolute path")
	}
	mt := strings.ToLower(strings.TrimSpace(doc.MediaType))
	if strings.HasPrefix(mt, "application/pdf") ||
		strings.Contains(mt, "wordprocessingml") ||
		strings.Contains(mt, "spreadsheetml") ||
		strings.Contains(mt, "officedocument") {
		return nil, fmt.Errorf("%w: parse function not configured", ErrKBIndexFailed)
	}
	raw, err := os.ReadFile(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKBIndexFailed, err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, fmt.Errorf("%w: empty body", ErrKBIndexFailed)
	}
	parts := SplitSearchableParts(doc.MediaType, text)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: no non-empty chunks", ErrKBIndexFailed)
	}
	if len(parts) > m8core.MaxKBChunksPerVersion {
		return nil, fmt.Errorf("%w: chunk count %d exceeds cap", ErrKBIndexFailed, len(parts))
	}
	return ChunksFromParts(doc, parts)
}

// SplitSearchableParts splits markdown/plain text on headings or rune runs.
func SplitSearchableParts(mediaType, text string) []string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if mt == "text/markdown" || mt == "text/plain" || mt == "" {
		if parts := splitMarkdownHeadings(text); len(parts) > 1 {
			return parts
		}
	}
	return splitByRunes(text, parseChunkRunes)
}

func splitMarkdownHeadings(text string) []string {
	lines := strings.Split(text, "\n")
	var parts []string
	var buf []string
	flush := func() {
		s := strings.TrimSpace(strings.Join(buf, "\n"))
		if s != "" {
			parts = append(parts, s)
		}
		buf = buf[:0]
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") && len(buf) > 0 {
			flush()
		}
		buf = append(buf, line)
	}
	flush()
	return parts
}

// ChunksFromParts builds body-carrying chunks for one document version.
func ChunksFromParts(doc m8core.KBDocument, parts []string) ([]m8core.KBChunk, error) {
	base := parseSourceLocator(doc.SourceLocator)
	out := make([]m8core.KBChunk, 0, len(parts))
	for i, part := range parts {
		part = capChunkBody(part)
		if strings.TrimSpace(part) == "" {
			continue
		}
		loc := map[string]any{
			"documentId": doc.DocumentID,
			"version":    doc.Version,
			"ordinal":    i,
			"page":       1,
			"quote":      trimRunes(part, 80),
		}
		for k, v := range base {
			if _, exists := loc[k]; !exists {
				loc[k] = v
			}
		}
		lb, err := json.Marshal(loc)
		if err != nil {
			return nil, err
		}
		out = append(out, m8core.KBChunk{
			ChunkID:     ulid.Make().String(),
			Body:        part,
			LocatorJSON: string(lb),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no non-empty chunks", ErrKBIndexFailed)
	}
	return out, nil
}

func capChunkBody(s string) string {
	if len(s) <= m8core.MaxKBChunkBody {
		return s
	}
	s = s[:m8core.MaxKBChunkBody]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func splitByRunes(text string, n int) []string {
	if n < 1 {
		n = parseChunkRunes
	}
	var parts []string
	var b strings.Builder
	count := 0
	for _, r := range text {
		b.WriteRune(r)
		count++
		if count >= n {
			parts = append(parts, strings.TrimSpace(b.String()))
			b.Reset()
			count = 0
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		parts = append(parts, s)
	}
	return parts
}

func trimRunes(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	var b strings.Builder
	i := 0
	for _, r := range s {
		if i >= n {
			break
		}
		b.WriteRune(r)
		i++
	}
	return b.String()
}

func parseSourceLocator(raw string) map[string]any {
	out := map[string]any{}
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "mro://") {
		return out
	}
	rest := strings.TrimPrefix(raw, "mro://")
	path, query, _ := strings.Cut(rest, "?")
	segs := strings.Split(path, "/")
	if len(segs) > 0 && segs[0] != "" {
		out["docType"] = segs[0]
	}
	if len(segs) > 1 && segs[1] != "" {
		out["revision"] = segs[1]
	}
	if query == "" {
		return out
	}
	for _, pair := range strings.Split(query, "&") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			continue
		}
		switch k {
		case "ata":
			out["ata"] = v
		case "status":
			out["status"] = v
		case "tail":
			out["tails"] = []string{v}
		}
	}
	return out
}
