package m8app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lunitide/lunitide/internal/doctext"
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
	ref := strings.TrimSpace(doc.ContentRef)
	if ref == "" || !filepath.IsAbs(ref) {
		return nil, fmt.Errorf("内容路径必须是绝对路径")
	}
	mt := strings.ToLower(strings.TrimSpace(doc.MediaType))
	if strings.HasPrefix(mt, "application/pdf") ||
		strings.Contains(mt, "wordprocessingml") ||
		strings.Contains(mt, "spreadsheetml") ||
		strings.Contains(mt, "officedocument") {
		return nil, fmt.Errorf("%w: 未配置正文解析", ErrKBIndexFailed)
	}
	raw, err := doctext.ReadSource(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKBIndexFailed, err)
	}
	if SourceDigest(raw) != doc.SHA256 {
		return nil, fmt.Errorf("%w: 源文件在入库后已被修改", ErrKBIndexFailed)
	}
	extracted, err := doctext.ExtractContext(ctx, ref, raw, doc.MediaType)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(extracted.Text)
	if text == "" {
		return nil, fmt.Errorf("%w: 没有可检索的正文", ErrKBIndexFailed)
	}
	parts := SplitSearchableParts(doc.MediaType, text)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: 没有可检索的正文", ErrKBIndexFailed)
	}
	if len(parts) > m8core.MaxKBChunksPerVersion {
		return nil, fmt.Errorf("%w: 分块数量超过上限", ErrKBIndexFailed)
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
			parts = append(parts, splitByRunes(s, parseChunkRunes)...)
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
		if len(part) > m8core.MaxKBChunkBody {
			return nil, fmt.Errorf("%w: 分块正文超过上限", ErrKBIndexFailed)
		}
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
		return nil, fmt.Errorf("%w: 没有可检索的正文", ErrKBIndexFailed)
	}
	return out, nil
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
