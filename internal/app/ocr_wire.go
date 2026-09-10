package app

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
)

func (e *Engine) ocrCredentialRef(providerID string) string {
	if e == nil || e.providers == nil || strings.TrimSpace(providerID) == "" {
		return ""
	}
	items, err := e.providers.List(context.Background(), provider.Filter{})
	if err != nil {
		return ""
	}
	for _, item := range items {
		if item.ID == providerID {
			return strings.TrimSpace(item.CredentialRef)
		}
	}
	return ""
}

func (e *Engine) ocrProviderCall(ctx context.Context, raw []byte, hint string) (string, error) {
	if e == nil || e.ocr == nil || e.providers == nil {
		return "", errors.New("OCR 供应商不可用")
	}
	if bytes.HasPrefix(raw, []byte("%PDF-")) {
		return "", errors.New("供应商 OCR 不处理原始 PDF，改走本地分页识别")
	}
	routing, err := e.ocr.Routing()
	if err != nil || !routing.Bound() {
		return "", errors.New("OCR 路由未绑定")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	var hit provider.Provider
	var model provider.Model
	found := false
	for _, p := range items {
		if p.ID != routing.ProviderID {
			continue
		}
		for _, m := range p.Models {
			if m.ModelID == routing.ModelID {
				hit, model, found = p, m, true
				break
			}
		}
	}
	if !found {
		return "", errors.New("OCR 绑定的模型不在当前目录")
	}
	mime := "image/png"
	switch {
	case bytes.HasPrefix(raw, []byte("\xff\xd8\xff")):
		mime = "image/jpeg"
	case bytes.HasPrefix(raw, []byte("GIF8")):
		mime = "image/gif"
	case bytes.HasPrefix(raw, []byte("RIFF")) && bytes.Contains(raw[:16], []byte("WEBP")):
		mime = "image/webp"
	}
	prompt := "Transcribe all visible text. Do not describe the scene. If no text is visible, say so."
	if strings.TrimSpace(hint) != "" {
		prompt += "\n" + hint
	}
	req := llmadapter.Request{
		Model:     model.ModelID,
		Messages:  []llmadapter.Message{{Role: llmadapter.RoleUser, Content: prompt}},
		Images:    []llmadapter.Image{{MIME: mime, Data: raw}},
		MaxTokens: 2048, MaxAttempts: 1,
	}
	var text string
	leaseErr := e.withProviderLease(ctx, hit, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		op = withCallPurpose(op, "ocr")
		a, adapterErr := e.adapterForModel(op, hit, model)
		if adapterErr != nil {
			return adapterErr
		}
		out, completeErr := a.Complete(op, secret, req)
		if completeErr != nil {
			return completeErr
		}
		text = strings.TrimSpace(out.Message.Content)
		if text == "" {
			return errors.New("OCR 供应商返回空结果")
		}
		return nil
	})
	return text, leaseErr
}

func (e *Engine) workspaceDocumentText(ctx context.Context, name string, raw []byte, media string) (string, string, string, int, error) {
	if e == nil || e.ocr == nil {
		return "", "", "", 0, errors.New("OCR 未装配")
	}
	got, err := e.ocr.RecognizeDocument(ctx, name, raw, media)
	if err != nil {
		return "", "", "", 0, err
	}
	kind := "pdf"
	if doctext.LooksLikeRasterImage(name, media, raw) {
		kind = "image"
		if got.Pages == 0 {
			got.Pages = 1
		}
	} else if !strings.HasSuffix(strings.ToLower(name), ".pdf") && !bytes.HasPrefix(raw, []byte("%PDF-")) {
		kind = "document"
	}
	method := strings.TrimSpace(got.Method)
	if !got.Complete {
		if method == "" {
			method = "incomplete-coverage"
		} else if !strings.Contains(method, "incomplete-coverage") {
			method += " incomplete-coverage"
		}
	}
	return got.Text, kind, method, got.Pages, nil
}
