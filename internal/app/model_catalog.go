package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

const visionDescribePrompt = "Describe the image(s) completely and transcribe all visible text (OCR). Be factual. Reply in the user's language if it is clear, otherwise Chinese."

func looksLikeOCRRequest(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, n := range []string{"识别", "ocr", "提取文字", "读出文字", "转成文字", "transcribe", "extract text"} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return false
}

func modelByID(p provider.Provider, id string) provider.Model {
	for _, m := range p.Models {
		if m.ModelID == id {
			return m
		}
	}
	return provider.Model{}
}

func injectVisionDescription(messages []llmadapter.Message, text string) []llmadapter.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return messages
	}
	block := "[视觉模型识别]\n" + text
	out := append([]llmadapter.Message(nil), messages...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == llmadapter.RoleUser {
			if strings.TrimSpace(out[i].Content) == "" {
				out[i].Content = block
			} else {
				out[i].Content = strings.TrimSpace(out[i].Content) + "\n\n" + block
			}
			return out
		}
	}
	return append(out, llmadapter.Message{Role: llmadapter.RoleUser, Content: block})
}

func lastUserContent(messages []llmadapter.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func (e *Engine) maybeDescribeImages(ctx context.Context, llm provider.Model, images []llmadapter.Image, userText string) (string, bool) {
	if len(images) == 0 {
		return "", false
	}
	if e.ocr != nil && looksLikeOCRRequest(userText) {
		for _, img := range images {
			got, err := e.ocr.RecognizeImage(ctx, img.Data)
			if err == nil && strings.TrimSpace(got.Text) != "" {
				return strings.TrimSpace(got.Text), true
			}
		}
	}
	if llm.SupportsVision || e.providers == nil {
		return "", false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", false
	}
	catalog := e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, llm.ModelID))
	if len(catalog) == 0 {
		return "", false
	}
	prompt := visionDescribePrompt
	if hint := strings.TrimSpace(userText); hint != "" {
		prompt += "\n\nUser message:\n" + hint
	}
	req := llmadapter.Request{
		Messages:    []llmadapter.Message{{Role: llmadapter.RoleUser, Content: prompt}},
		Images:      images,
		MaxTokens:   2048,
		MaxAttempts: 1,
	}
	for _, entry := range catalog {
		req.Model = entry.Model.ModelID
		req.Messages = []llmadapter.Message{{Role: llmadapter.RoleUser, Content: prompt}}
		if strings.Contains(strings.ToLower(req.Model), "deepseek-ocr") {
			req.Messages = append([]llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "<image>\nFree OCR."}}, req.Messages...)
		}
		var text string
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			op = withCallPurpose(op, "vision")
			a, adapterErr := e.adapterForModel(op, entry.Provider, entry.Model)
			if adapterErr != nil {
				return adapterErr
			}
			out, completeErr := a.Complete(op, secret, req)
			if completeErr != nil {
				return completeErr
			}
			if out.FinishReason == "length" || out.FinishReason == "content_filter" {
				return fmt.Errorf("vision recognition incomplete: %s", out.FinishReason)
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

func (e *Engine) invokeMediaGenerate(ctx context.Context, sessionID, name string, args json.RawMessage) (out toolruntime.Result, err error) {
	var a struct {
		Prompt string `json:"prompt"`
		Path   string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.Prompt) == "" {
		return toolruntime.Result{}, fmt.Errorf("invalid %s arguments", name)
	}
	if existing, ok := e.findReusableMediaOp(ctx, sessionID, name, args); ok {
		return existingMediaResult(existing), nil
	}
	kind := provider.KindImage
	if name == "video.generate" {
		kind = provider.KindVideo
	}
	if e.providers == nil {
		return toolruntime.Result{}, fmt.Errorf("没有配置%s供应商", mediaKindLabel(kind))
	}
	items, listErr := e.providers.List(ctx, provider.Filter{})
	if listErr != nil {
		return toolruntime.Result{}, listErr
	}
	catalog := provider.CatalogForKind(items, kind)
	if len(catalog) == 0 {
		return toolruntime.Result{}, fmt.Errorf("没有启用的%s（设置 → 模型与供应商）", mediaKindLabel(kind))
	}
	rec, recErr := e.beginToolOperation(ctx, sessionID, name, args)
	if recErr != nil {
		return toolruntime.Result{}, recErr
	}
	defer rec.finish(&out, &err)
	var last error
	for _, entry := range catalog {
		var summary string
		var generated llmadapter.MediaResult
		var submitted bool
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			aAdapter, adapterErr := e.adapterForModel(op, entry.Provider, entry.Model)
			if adapterErr != nil {
				return adapterErr
			}
			prompt := strings.TrimSpace(a.Prompt)
			if kind == provider.KindVideo {
				if _, ok := adapterAs[llmadapter.VideoGenerator](aAdapter); !ok {
					return fmt.Errorf("%s %s does not support video generation", entry.Provider.Name, entry.Model.ModelID)
				}
				submitted = true
				rec.noteSubmitted()
				videoOut, genErr := generateVideoThrough(op, aAdapter, secret, entry.Model.ModelID, prompt)
				if genErr != nil {
					return genErr
				}
				rec.bindTrack(mediaExternalID(videoOut), "", nil)
				summary = formatMediaResult(kind, entry, videoOut)
				generated = videoOut
				return nil
			}
			if _, ok := adapterAs[llmadapter.ImageGenerator](aAdapter); !ok {
				return fmt.Errorf("%s %s does not support image generation", entry.Provider.Name, entry.Model.ModelID)
			}
			submitted = true
			rec.noteSubmitted()
			imageOut, genErr := generateImageThrough(op, aAdapter, secret, entry.Model.ModelID, prompt)
			if genErr != nil {
				return genErr
			}
			rec.bindTrack(mediaExternalID(imageOut), "", nil)
			summary = formatMediaResult(kind, entry, imageOut)
			generated = imageOut
			return nil
		})
		if leaseErr == nil && summary != "" {
			// Delivery failure must not resubmit a potentially paid generation.
			if len(generated.Data) > 0 && kind == provider.KindImage {
				if e.tools == nil || sessionID == "" {
					return toolruntime.Result{}, fmt.Errorf("图片已生成，但会话文件存储不可用")
				}
				path := strings.TrimSpace(a.Path)
				if path == "" {
					path = "generated-" + ulid.Make().String() + ".png"
				}
				written, saveErr := e.tools.SaveGeneratedImage(sessionID, path, generated.Data)
				if saveErr != nil {
					return toolruntime.Result{}, fmt.Errorf("图片保存失败（未重复生成）: %w", saveErr)
				}
				if written.Artifact != nil && strings.TrimSpace(written.Artifact.Path) != "" {
					rec.bindTrack("", "", []string{written.Artifact.Path})
				}
				return written, nil
			}
			link, parseErr := url.Parse(generated.URL)
			if parseErr != nil || link.Hostname() == "" || (link.Scheme != "https" && link.Scheme != "http") || link.User != nil {
				return toolruntime.Result{}, fmt.Errorf("模型未返回可打开的生成结果")
			}
			if strings.TrimSpace(a.Path) != "" {
				summary += "\n返回的是在线地址，尚未保存到请求的本地路径。"
			}
			return toolruntime.Result{Output: summary}, nil
		}
		// Media submissions may already be billable after an uncertain error.
		// Endpoint compatibility is handled inside the adapter before acceptance.
		if leaseErr != nil && (submitted || kind == provider.KindVideo) {
			rec.markUnknown()
			return toolruntime.Result{}, leaseErr
		}
		last = leaseErr
	}
	if last == nil {
		last = fmt.Errorf("%s backups exhausted", mediaKindLabel(kind))
	}
	return toolruntime.Result{}, last
}

func mediaExternalID(out llmadapter.MediaResult) string {
	if id := strings.TrimSpace(out.ID); id != "" && len(id) <= 256 {
		return id
	}
	if u := strings.TrimSpace(out.URL); u != "" && len(u) <= 256 {
		return u
	}
	return ""
}

func mediaOpBlocksResubmit(op modelfit.ToolOperation) bool {
	if op.State == modelfit.OpCancelled {
		return false
	}
	if strings.TrimSpace(op.ExternalID) != "" || strings.TrimSpace(op.EvidenceRef) == "submitted" {
		return true
	}
	switch op.State {
	case modelfit.OpPending, modelfit.OpRunning, modelfit.OpUnknown, modelfit.OpSucceeded:
		return true
	default:
		return false
	}
}

func (e *Engine) findReusableMediaOp(ctx context.Context, sessionID, name string, args json.RawMessage) (modelfit.ToolOperation, bool) {
	if e == nil || e.toolOps == nil {
		return modelfit.ToolOperation{}, false
	}
	digest := argsDigestOrFallback(name, args)
	ops, err := e.toolOps.ListToolOperations(ctx, ownerScope(sessionID), 50)
	if err != nil {
		return modelfit.ToolOperation{}, false
	}
	var best modelfit.ToolOperation
	found := false
	for _, op := range ops {
		if op.ToolName != name || op.InputDigest != digest || !mediaOpBlocksResubmit(op) {
			continue
		}
		if !found || op.UpdatedAt.After(best.UpdatedAt) {
			best = op
			found = true
		}
	}
	return best, found
}

func existingMediaResult(op modelfit.ToolOperation) toolruntime.Result {
	var b strings.Builder
	switch {
	case op.State == modelfit.OpSucceeded:
		b.WriteString("已有完成结果，未重复生成。")
	case strings.TrimSpace(op.ExternalID) != "":
		b.WriteString("已有远端任务，未重复生成。请查询现有结果。")
	default:
		b.WriteString("上次提交结果未知，未重复生成。请先核实，不要重新付费提交。")
	}
	if id := strings.TrimSpace(op.ExternalID); id != "" {
		fmt.Fprintf(&b, " externalId=%s", id)
	}
	return toolruntime.Result{Output: b.String()}
}

func mediaKindLabel(kind provider.Kind) string {
	switch kind {
	case provider.KindVideo:
		return "生视频模型"
	default:
		return "生图模型"
	}
}

func formatMediaResult(kind provider.Kind, entry provider.CatalogEntry, out llmadapter.MediaResult) string {
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
