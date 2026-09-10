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
	complete     bool
	uncertain    bool
}
type officeTextCache struct {
	mu     sync.Mutex
	values map[string]officeText
}

func officeTextCacheKey(sha, ocrRevision string) string {
	key := strings.TrimSpace(sha)
	if key == "" {
		return ""
	}
	if rev := strings.TrimSpace(ocrRevision); rev != "" {
		return key + "\x00ocr:" + rev
	}
	return key
}

func (e *Engine) officeTextCacheKey(version domain.Version) string {
	rev := ""
	if e != nil && e.ocr != nil {
		if routing, err := e.ocr.Routing(); err == nil {
			rev = routing.Revision
		}
	}
	return officeTextCacheKey(version.SHA256, rev)
}

func (e *Engine) officeVersionText(ctx context.Context, version domain.Version, data []byte) (officeText, error) {
	// Cache by content + OCR routing revision. Same SHA under a new OCR
	// strategy must not reuse old text (HAT-26 / C04).
	cacheKey := e.officeTextCacheKey(version)
	e.officeTexts.mu.Lock()
	cached, ok := e.officeTexts.values[cacheKey]
	e.officeTexts.mu.Unlock()
	if ok {
		return cached, nil
	}
	if version.Kind == "pdf" && e != nil && e.ocr != nil {
		got, ocrErr := e.ocr.RecognizePDF(ctx, data)
		if ocrErr != nil {
			return officeText{}, fmt.Errorf("PDF 无法读取，识别未能完成：%w", ocrErr)
		}
		result := officeText{text: got.Text, method: got.Method, pages: got.Pages, complete: got.Complete, uncertain: got.Uncertain}
		if cacheKey != "" && len(result.text) <= 1<<20 {
			e.officeTexts.mu.Lock()
			if len(e.officeTexts.values) >= 8 {
				e.officeTexts.values = nil
			}
			if e.officeTexts.values == nil {
				e.officeTexts.values = make(map[string]officeText)
			}
			e.officeTexts.values[cacheKey] = result
			e.officeTexts.mu.Unlock()
		}
		return result, nil
	}
	extracted, err := doctext.ExtractContext(ctx, version.Name, data, version.MediaType)
	result := officeText{text: extracted.Text, method: "text-layer", complete: pdfTextLayerComplete(version.Kind, data, err)}
	if version.Kind == "pdf" && errors.Is(err, doctext.ErrNoTextLayer) {
		if e == nil || e.ocr == nil {
			return officeText{}, errors.New("OCR 未装配，无法处理扫描 PDF")
		}
		ocr, ocrErr := doctext.ExtractPDFOCR(ctx, data)
		if ocrErr != nil {
			return officeText{}, fmt.Errorf("PDF 无文字层，识别未能完成：%w", ocrErr)
		}
		var b strings.Builder
		for _, page := range ocr.Pages {
			fmt.Fprintf(&b, "\n[PDF page %d; OCR]\n%s\n", page.Page, page.Text)
		}
		result = officeText{text: b.String(), method: ocr.Method, pages: len(ocr.Pages), complete: false, uncertain: true}
	} else if err != nil {
		return officeText{}, err
	}
	if cacheKey != "" && len(result.text) <= 1<<20 {
		e.officeTexts.mu.Lock()
		if len(e.officeTexts.values) >= 8 {
			e.officeTexts.values = nil
		}
		if e.officeTexts.values == nil {
			e.officeTexts.values = make(map[string]officeText)
		}
		e.officeTexts.values[cacheKey] = result
		e.officeTexts.mu.Unlock()
	}
	return result, nil
}

func pdfTextLayerComplete(kind string, data []byte, extractErr error) bool {
	if extractErr != nil {
		return false
	}
	if kind != "pdf" {
		return true
	}
	pages, err := doctext.ExtractPDFPages(data)
	if err != nil || len(pages) == 0 {
		return false
	}
	for _, p := range pages {
		if strings.TrimSpace(p.Text) == "" {
			return false
		}
	}
	return true
}

func officeSourceTextPage(taskID string, version domain.Version, source officeText, offset int) (string, error) {
	text := []rune(source.text)
	if offset < 0 || offset > len(text) {
		return "", domain.ErrInvalid
	}
	page := map[string]any{"taskId": taskID, "versionId": version.ID, "sha256": version.SHA256, "kind": version.Kind, "view": "text", "method": source.method, "textOffset": offset, "totalRunes": len(text), "pageCount": source.pages, "coverageComplete": source.complete}
	switch {
	case !source.complete:
		page["notice"] = "OCR coverage is incomplete. Unread pages are unrecognized, not proof of blank content. May omit or misread text/numbers; prefer a supplied editable original."
	case source.method != "" && source.method != "text-layer":
		page["notice"] = "OCR routing result (provider and/or local), not a verified text layer. May omit or misread text/numbers; prefer a supplied editable original. Empty pages are unrecognized, not proof of blank content."
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
