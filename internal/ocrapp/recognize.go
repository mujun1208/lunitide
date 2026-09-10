package ocrapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/mediajob"
)

type Source string

const (
	SourceTextLayer Source = "text-layer"
	SourceProvider  Source = "provider"
	SourceLocal     Source = "local"
	SourceMixed     Source = "mixed"
	SourceUnknown   Source = "unknown"
)

type PageCoverage struct {
	Page      int    `json:"page"`
	Source    Source `json:"source"`
	HasText   bool   `json:"hasText"`
	Uncertain bool   `json:"uncertain"`
}

type Result struct {
	Text      string         `json:"text"`
	Method    string         `json:"method"`
	Source    Source         `json:"source"`
	Pages     int            `json:"pages"`
	Coverage  []PageCoverage `json:"coverage,omitempty"`
	Complete  bool           `json:"complete"`
	Uncertain bool           `json:"uncertain"`
}

type ProviderFunc func(ctx context.Context, raw []byte, hint string) (text string, err error)
type LocalPDFFunc func(ctx context.Context, raw []byte) (doctext.PDFOCRResult, error)
type LocalImageFunc func(ctx context.Context, raw []byte) (doctext.PDFOCRResult, error)
type RenderPDFFunc func(ctx context.Context, raw []byte, pages []int) ([]doctext.RenderedPDFPage, error)
type CredentialFunc func(providerID string) string

type ocrFill struct {
	Page   int
	Text   string
	Source Source
	Method string
}

type Service struct {
	store      *FileStore
	provider   ProviderFunc
	localPDF   LocalPDFFunc
	localImage LocalImageFunc
	renderPDF  RenderPDFFunc
	credential CredentialFunc
	now        func() time.Time
	healthMu   sync.Mutex
	health     map[string]healthNote
}

func New(store *FileStore) *Service {
	s := &Service{store: store, localPDF: doctext.ExtractPDFOCR, localImage: doctext.ExtractImageOCR, renderPDF: doctext.RenderPDFPages, now: time.Now}
	s.loadHealth()
	return s
}

func (s *Service) SetProvider(fn ProviderFunc) { s.provider = fn }
func (s *Service) SetLocalPDF(fn LocalPDFFunc) {
	if s != nil {
		s.localPDF = fn
	}
}
func (s *Service) SetLocalImage(fn LocalImageFunc) {
	if s != nil {
		s.localImage = fn
	}
}
func (s *Service) SetRenderPDF(fn RenderPDFFunc) {
	if s != nil {
		s.renderPDF = fn
	}
}
func (s *Service) SetCredential(fn CredentialFunc) {
	if s != nil {
		s.credential = fn
	}
}

func (s *Service) Routing() (Routing, error) {
	if s == nil || s.store == nil {
		return Routing{PreferProvider: true, Revision: RoutingRevision(Routing{PreferProvider: true})}, nil
	}
	return s.store.Get()
}

func (s *Service) SetRouting(next Routing, expected string) (Routing, error) {
	if s == nil || s.store == nil {
		return Routing{}, errors.New("OCR 路由存储不可用")
	}
	saved, err := s.store.CompareAndSet(next, expected)
	if err == nil {
		s.clearHealth()
	}
	return saved, err
}

func providerErrorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401") || strings.Contains(msg, "forbidden") || strings.Contains(msg, "403"):
		return "auth"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate"):
		return "rate"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") || strings.Contains(msg, "unavailable") || strings.Contains(msg, "503") || strings.Contains(msg, "connection"):
		return "unavailable"
	default:
		return "failed"
	}
}

func (s *Service) RecognizeDocument(ctx context.Context, name string, raw []byte, media string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	kind := ""
	if strings.HasSuffix(strings.ToLower(name), ".pdf") || strings.Contains(strings.ToLower(media), "pdf") || strings.HasPrefix(string(raw), "%PDF-") {
		return s.RecognizePDF(ctx, raw)
	}
	if doctext.LooksLikeRasterImage(name, media, raw) {
		return s.RecognizeImage(ctx, raw)
	}
	extracted, err := doctext.ExtractContext(ctx, name, raw, media)
	if err == nil {
		return Result{Text: extracted.Text, Method: "text-layer", Source: SourceTextLayer, Complete: true}, nil
	}
	if !errors.Is(err, doctext.ErrNoTextLayer) {
		return Result{}, err
	}
	_ = kind
	return Result{}, err
}

func (s *Service) RecognizePDF(ctx context.Context, raw []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	pages, err := doctext.ExtractPDFPages(raw)
	if err != nil && !errors.Is(err, doctext.ErrNoTextLayer) {
		if errors.Is(err, doctext.ErrUnsupportedFormat) || errors.Is(err, doctext.ErrBudgetExceeded) {
			return Result{}, err
		}
		pages = nil
	}
	out := make([]doctext.PDFPageText, len(pages))
	copy(out, pages)
	coverage := make([]PageCoverage, 0, len(out))
	needOCR := false
	for _, p := range out {
		has := strings.TrimSpace(p.Text) != ""
		coverage = append(coverage, PageCoverage{Page: p.Page, Source: SourceTextLayer, HasText: has})
		if !has {
			needOCR = true
		}
	}
	if len(out) > 0 && !needOCR {
		return Result{
			Text: strings.TrimSpace(doctext.JoinPDFPages(out)), Method: "text-layer",
			Source: SourceTextLayer, Pages: len(out), Coverage: coverage, Complete: true,
		}, nil
	}
	empty := make([]int, 0)
	hasLayer := false
	batch := make([]BatchPage, 0, len(out))
	for _, p := range out {
		has := strings.TrimSpace(p.Text) != ""
		if has {
			hasLayer = true
		}
		batch = append(batch, BatchPage{Page: p.Page, HasText: has})
	}
	for _, p := range IncompletePages(batch) {
		empty = append(empty, p.Page)
	}
	fills, method, src, ocrErr := s.recognizeMissing(ctx, raw, empty, hasLayer && len(empty) > 0)
	if errors.Is(ocrErr, context.Canceled) || errors.Is(ocrErr, context.DeadlineExceeded) {
		return Result{}, ocrErr
	}
	if ocrErr != nil && len(out) == 0 {
		return Result{}, ocrErr
	}
	byPage := map[int]ocrFill{}
	for _, p := range fills {
		byPage[p.Page] = p
	}
	if len(out) == 0 {
		for _, p := range fills {
			out = append(out, doctext.PDFPageText{Page: p.Page, Text: p.Text})
			coverage = append(coverage, PageCoverage{Page: p.Page, Source: p.Source, HasText: strings.TrimSpace(p.Text) != "", Uncertain: true})
		}
	} else {
		for i := range out {
			if strings.TrimSpace(out[i].Text) != "" {
				continue
			}
			if fill, ok := byPage[out[i].Page]; ok && strings.TrimSpace(fill.Text) != "" {
				out[i].Text = fill.Text
				coverage[i].Source = fill.Source
				coverage[i].HasText = true
				coverage[i].Uncertain = true
			}
		}
	}
	complete := true
	uncertain := false
	usedOCR := false
	usedLayer := false
	for _, c := range coverage {
		if !c.HasText {
			complete = false
		}
		if c.Uncertain {
			uncertain = true
		}
		if c.Source == SourceProvider || c.Source == SourceLocal {
			usedOCR = true
		}
		if c.Source == SourceTextLayer && c.HasText {
			usedLayer = true
		}
	}
	source := src
	if usedLayer && usedOCR {
		source = SourceMixed
	} else if usedLayer && !usedOCR {
		source = SourceTextLayer
		method = "text-layer"
	}
	if method == "" {
		method = string(source)
	}
	if len(out) == 0 {
		return Result{}, fmt.Errorf("PDF 无文字层，识别未能完成：%w", ocrErr)
	}
	return Result{
		Text: strings.TrimSpace(doctext.JoinPDFPages(out)), Method: method,
		Source: source, Pages: len(out), Coverage: coverage, Complete: complete, Uncertain: uncertain,
	}, nil
}

func (s *Service) recognizeMissing(ctx context.Context, raw []byte, empty []int, mixed bool) ([]ocrFill, string, Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", SourceUnknown, err
	}
	if s.renderPDF != nil && len(empty) > 0 {
		rendered, err := s.renderPDF(ctx, raw, empty)
		if ctx.Err() != nil {
			return nil, "", SourceUnknown, ctx.Err()
		}
		if err == nil && len(rendered) > 0 {
			return s.recognizeRenderedPages(ctx, rendered)
		}
		if mixed {
			return nil, "", SourceUnknown, nil
		}
	}
	if mixed {
		return nil, "", SourceUnknown, nil
	}
	if s.localPDF == nil {
		return nil, "", SourceUnknown, errors.New("本地 OCR 未装配")
	}
	got, localErr := s.localPDF(ctx, raw)
	if ctx.Err() != nil {
		return nil, "", SourceUnknown, ctx.Err()
	}
	if localErr != nil {
		return nil, "", SourceLocal, localErr
	}
	fills := make([]ocrFill, 0, len(got.Pages))
	for _, page := range got.Pages {
		fills = append(fills, ocrFill{Page: page.Page, Text: page.Text, Source: SourceLocal, Method: got.Method})
	}
	return fills, got.Method, SourceLocal, nil
}

func (s *Service) recognizeRenderedPages(ctx context.Context, rendered []doctext.RenderedPDFPage) ([]ocrFill, string, Source, error) {
	fills := make([]ocrFill, 0, len(rendered))
	method := ""
	src := SourceUnknown
	for _, idx := range mediajob.Remaining(mediajob.Journal{Total: len(rendered), Cancel: ctx.Err() != nil}) {
		if err := ctx.Err(); err != nil {
			return nil, "", SourceUnknown, err
		}
		page := rendered[idx]
		text, err := s.tryProvider(ctx, page.PNG, "image-ocr")
		if err == nil {
			fills = append(fills, ocrFill{Page: page.Page, Text: text, Source: SourceProvider, Method: "provider-ocr"})
			method = "provider-ocr"
			if src == SourceUnknown {
				src = SourceProvider
			} else if src != SourceProvider {
				src = SourceMixed
			}
			continue
		}
		if ctx.Err() != nil {
			return nil, "", SourceUnknown, ctx.Err()
		}
		if s.localImage == nil {
			continue
		}
		got, localErr := s.localImage(ctx, page.PNG)
		if ctx.Err() != nil {
			return nil, "", SourceUnknown, ctx.Err()
		}
		if localErr != nil {
			continue
		}
		text = firstLocalImageText(got)
		if text == "" {
			continue
		}
		pageMethod := strings.TrimSpace(got.Method)
		if pageMethod == "" {
			pageMethod = "windows-ocr"
		}
		fills = append(fills, ocrFill{Page: page.Page, Text: text, Source: SourceLocal, Method: pageMethod})
		if method == "" {
			method = pageMethod
		}
		if src == SourceUnknown {
			src = SourceLocal
		} else if src != SourceLocal {
			src = SourceMixed
		}
	}
	return fills, method, src, nil
}

func firstLocalImageText(got doctext.PDFOCRResult) string {
	var b strings.Builder
	for _, page := range got.Pages {
		if strings.TrimSpace(page.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(page.Text))
	}
	return b.String()
}

func (s *Service) RecognizeImage(ctx context.Context, raw []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	text, err := s.tryProvider(ctx, raw, "image-ocr")
	if err == nil {
		return Result{Text: text, Method: "provider-ocr", Source: SourceProvider, Complete: true, Uncertain: true}, nil
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if s.localImage == nil {
		return Result{}, errors.New("本地图片识别未装配，请配置 OCR 路由或改用视觉模型")
	}
	got, localErr := s.localImage(ctx, raw)
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if localErr != nil {
		return Result{}, localErr
	}
	var b strings.Builder
	for _, page := range got.Pages {
		if strings.TrimSpace(page.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(page.Text))
	}
	method := strings.TrimSpace(got.Method)
	if method == "" {
		method = "windows-ocr"
	}
	pages := len(got.Pages)
	if pages == 0 {
		pages = 1
	}
	return Result{Text: b.String(), Method: method, Source: SourceLocal, Pages: pages, Complete: true, Uncertain: true}, nil
}

func (s *Service) ReadDocument(ctx context.Context, name string, raw []byte, media string) (text, kind, method string, pages int, err error) {
	got, err := s.RecognizeDocument(ctx, name, raw, media)
	if err != nil {
		return "", "", "", 0, err
	}
	kind = "pdf"
	if doctext.LooksLikeRasterImage(name, media, raw) {
		kind = "image"
		if got.Pages == 0 {
			got.Pages = 1
		}
	} else if !strings.HasSuffix(strings.ToLower(name), ".pdf") && !strings.HasPrefix(string(raw), "%PDF-") {
		kind = "document"
	}
	return got.Text, kind, got.Method, got.Pages, nil
}
