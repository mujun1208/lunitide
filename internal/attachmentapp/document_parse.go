package attachmentapp

import (
	"bytes"
	"context"
	"mime"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/doctext"
)

// Binary documents share the existing bounded parser worker. A failed
// extraction retains the original attachment, but never feeds binary bytes
// or a made-up summary into the conversation's readable context.
func (s *Service) parseDocument(ctx context.Context, name, mime string, data []byte) (string, error) {
	mime = attachmentTextMIME(name, mime, data)
	ext := strings.ToLower(filepath.Ext(name))
	document := ext == ".docx" || ext == ".pptx" || ext == ".xlsx" || ext == ".pdf"
	if !document {
		document = mime == "application/pdf" || strings.HasPrefix(mime, "application/vnd.openxmlformats-officedocument.")
	}
	if !document {
		return s.parse(mime, data)
	}
	r, err := doctext.ExtractContext(ctx, name, data, mime)
	if err != nil {
		return "", err
	}
	return r.Text, nil
}

// Browsers/Windows often label TypeScript as MPEG transport video, and JSON or
// source files as application/octet-stream. Recognize known textual extensions
// only after validating the complete payload; never inject binary bytes.
func attachmentTextMIME(name, declared string, data []byte) string {
	declared = strings.ToLower(strings.TrimSpace(declared))
	if mediaType, _, err := mime.ParseMediaType(declared); err == nil {
		declared = mediaType
	}
	ext := strings.ToLower(filepath.Ext(name))
	textual := false
	switch ext {
	case ".txt", ".md", ".markdown", ".json", ".jsonc", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".go", ".rs", ".java", ".cs", ".css", ".html", ".yaml", ".yml", ".toml", ".ini", ".sql", ".sh", ".ps1", ".csv", ".log":
		textual = true
	}
	if !textual || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return declared
	}
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\r' && b != '\n' {
			return declared
		}
	}
	if declared == "" || declared == "application/octet-stream" || (ext == ".ts" && declared == "video/mp2t") || declared == "application/typescript" {
		if ext == ".json" || ext == ".jsonc" {
			return "application/json"
		}
		return "text/plain"
	}
	return declared
}
