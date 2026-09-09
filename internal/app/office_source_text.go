package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/doctext"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

type officeText struct {
	text, method string
	pages        int
}
type officeTextCache struct {
	mu     sync.Mutex
	values map[string]officeText
}

func (e *Engine) officeVersionText(ctx context.Context, version domain.Version, data []byte) (officeText, error) {
	// Every caller has already read the scoped immutable version. Cache by content,
	// never by filename, so replacing an upload cannot reuse its previous text.
	e.officeTexts.mu.Lock()
	cached, ok := e.officeTexts.values[version.SHA256]
	e.officeTexts.mu.Unlock()
	if ok {
		return cached, nil
	}
	extracted, err := doctext.ExtractContext(ctx, version.Name, data, version.MediaType)
	result := officeText{text: extracted.Text, method: "text-layer"}
	if version.Kind == "pdf" && errors.Is(err, doctext.ErrNoTextLayer) {
		ocr, ocrErr := doctext.ExtractPDFOCR(ctx, data)
		if ocrErr != nil {
			return officeText{}, fmt.Errorf("PDF text layer unavailable; local OCR could not complete: %w", ocrErr)
		}
		var b strings.Builder
		for _, page := range ocr.Pages {
			fmt.Fprintf(&b, "\n[PDF page %d; OCR]\n%s\n", page.Page, page.Text)
		}
		result = officeText{text: b.String(), method: ocr.Method, pages: len(ocr.Pages)}
	} else if err != nil {
		return officeText{}, err
	}
	if version.SHA256 != "" && len(result.text) <= 1<<20 {
		e.officeTexts.mu.Lock()
		if len(e.officeTexts.values) >= 8 {
			e.officeTexts.values = nil
		}
		if e.officeTexts.values == nil {
			e.officeTexts.values = make(map[string]officeText)
		}
		e.officeTexts.values[version.SHA256] = result
		e.officeTexts.mu.Unlock()
	}
	return result, nil
}

func officeSourceTextPage(taskID string, version domain.Version, source officeText, offset int) (string, error) {
	text := []rune(source.text)
	if offset < 0 || offset > len(text) {
		return "", domain.ErrInvalid
	}
	page := map[string]any{"taskId": taskID, "versionId": version.ID, "sha256": version.SHA256, "kind": version.Kind, "view": "text", "method": source.method, "textOffset": offset, "totalRunes": len(text), "pageCount": source.pages}
	if source.method == "windows-ocr" {
		page["notice"] = "Local OCR of rendered pages, not a verified text layer. May omit or misread text/numbers; prefer a supplied editable original. Empty pages are unrecognized, not proof of blank content."
	}
	// Fit the encoded JSON, including escaping, instead of clipping text bytes.
	lo, hi := offset, min(len(text), offset+3000)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		page["text"], page["nextTextOffset"], page["hasMore"] = string(text[offset:mid]), mid, mid < len(text)
		b, err := json.Marshal(page)
		if err != nil {
			return "", err
		}
		if len(b) <= officeToolPageLimit-64 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	page["text"], page["nextTextOffset"], page["hasMore"] = string(text[offset:lo]), lo, lo < len(text)
	if lo == offset && offset < len(text) {
		return "", domain.ErrInvalid
	}
	return officeToolJSON(page)
}
