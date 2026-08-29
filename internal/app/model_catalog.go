package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const visionDescribePrompt = "Describe the image(s) completely and transcribe all visible text (OCR). Be factual. Reply in the user's language if it is clear, otherwise Chinese."

func modelByID(p provider.Provider, id string) provider.Model {
	for _, m := range p.Models {
		if m.ModelID == id {
			return m
		}
	}
	return provider.Model{}
}

func injectVisionDescription(messages []gateway.Message, text string) []gateway.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return messages
	}
	block := "[视觉模型识别]\n" + text
	out := append([]gateway.Message(nil), messages...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == gateway.RoleUser {
			if strings.TrimSpace(out[i].Content) == "" {
				out[i].Content = block
			} else {
				out[i].Content = strings.TrimSpace(out[i].Content) + "\n\n" + block
			}
			return out
		}
	}
	return append(out, gateway.Message{Role: gateway.RoleUser, Content: block})
}

func lastUserContent(messages []gateway.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == gateway.RoleUser {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func (e *Engine) maybeDescribeImages(ctx context.Context, llm provider.Model, images []gateway.Image, userText string) (string, bool) {
	if len(images) == 0 || llm.SupportsVision || e.providers == nil {
		return "", false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", false
	}
	catalog := provider.VisionDescribeCatalog(items, llm.ModelID)
	if len(catalog) == 0 {
		return "", false
	}
	prompt := visionDescribePrompt
	if hint := strings.TrimSpace(userText); hint != "" {
		prompt += "\n\nUser message:\n" + hint
	}
	req := gateway.Request{
		Messages:    []gateway.Message{{Role: gateway.RoleUser, Content: prompt}},
		Images:      images,
		MaxTokens:   2048,
		MaxAttempts: 1,
	}
	for _, entry := range catalog {
		req.Model = entry.Model.ModelID
		var text string
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			a, adapterErr := e.adapter(op, entry.Provider)
			if adapterErr != nil {
				return adapterErr
			}
			out, completeErr := a.Complete(op, secret, req)
			if completeErr != nil {
				return completeErr
			}
			text = strings.TrimSpace(out.Message.Content)
			if text == "" {
				return fmt.Errorf("empty vision description")
			}
			return nil
		})
		if leaseErr == nil && text != "" {
			return text, true
		}
	}
	return "", false
}

func (e *Engine) invokeMediaGenerate(ctx context.Context, name string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		Prompt string `json:"prompt"`
		Path   string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.Prompt) == "" {
		return toolruntime.Result{}, fmt.Errorf("invalid %s arguments", name)
	}
	kind := provider.KindImage
	if name == "video.generate" {
		kind = provider.KindVideo
	}
	if e.providers == nil {
		return toolruntime.Result{}, fmt.Errorf("没有配置%s供应商", mediaKindLabel(kind))
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return toolruntime.Result{}, err
	}
	catalog := provider.CatalogForKind(items, kind)
	if len(catalog) == 0 {
		return toolruntime.Result{}, fmt.Errorf("没有启用的%s（设置 → 模型与供应商）", mediaKindLabel(kind))
	}
	var last error
	for _, entry := range catalog {
		var summary string
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			aAdapter, adapterErr := e.adapter(op, entry.Provider)
			if adapterErr != nil {
				return adapterErr
			}
			prompt := strings.TrimSpace(a.Prompt)
			if kind == provider.KindVideo {
				gen, ok := aAdapter.(gateway.VideoGenerator)
				if !ok {
					return fmt.Errorf("%s %s does not support video generation", entry.Provider.Name, entry.Model.ModelID)
				}
				out, genErr := gen.GenerateVideo(op, secret, entry.Model.ModelID, prompt)
				if genErr != nil {
					return genErr
				}
				summary = formatMediaResult(kind, entry, out)
				return nil
			}
			gen, ok := aAdapter.(gateway.ImageGenerator)
			if !ok {
				return fmt.Errorf("%s %s does not support image generation", entry.Provider.Name, entry.Model.ModelID)
			}
			out, genErr := gen.GenerateImage(op, secret, entry.Model.ModelID, prompt)
			if genErr != nil {
				return genErr
			}
			summary = formatMediaResult(kind, entry, out)
			return nil
		})
		if leaseErr == nil && summary != "" {
			if path := strings.TrimSpace(a.Path); path != "" {
				summary += "\nrequestedPath=" + path
			}
			return toolruntime.Result{Output: summary}, nil
		}
		last = leaseErr
	}
	if last == nil {
		last = fmt.Errorf("%s backups exhausted", mediaKindLabel(kind))
	}
	return toolruntime.Result{}, last
}

func mediaKindLabel(kind provider.Kind) string {
	switch kind {
	case provider.KindVideo:
		return "生视频模型"
	default:
		return "生图模型"
	}
}

func formatMediaResult(kind provider.Kind, entry provider.CatalogEntry, out gateway.MediaResult) string {
	label := mediaKindLabel(kind)
	var b strings.Builder
	fmt.Fprintf(&b, "已用%s %s / %s 生成。", label, entry.Provider.Name, entry.Model.ModelID)
	if out.URL != "" {
		fmt.Fprintf(&b, " url=%s", out.URL)
	}
	if out.ID != "" {
		fmt.Fprintf(&b, " id=%s", out.ID)
	}
	if len(out.Data) > 0 {
		fmt.Fprintf(&b, " bytes=%d mime=%s", len(out.Data), out.MIME)
	}
	return b.String()
}
