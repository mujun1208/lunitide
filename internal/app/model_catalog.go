package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

const visionDescribePrompt = "Describe the image(s) completely and transcribe all visible text (OCR). Be factual. Reply in the user's language if it is clear, otherwise Chinese."

func (e *Engine) attachedImageOCRText(ctx context.Context, images []llmadapter.Image) string {
	if e.ocr == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var b strings.Builder
	for i, img := range images {
		got, err := e.ocr.RecognizeImage(ctx, img.Data)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(got.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if len(images) > 1 {
			fmt.Fprintf(&b, "图片%d：%s", i+1, text)
			continue
		}
		b.WriteString(text)
	}
	return b.String()
}

// oversizedImageOCR reads a screenshot the vision budget refused and runs the
// same provider → RapidOCR → Windows OCR stack used for smaller images.
func (e *Engine) oversizedImageOCR(ctx context.Context, imageRef attachment.Attachment) string {
	if e == nil || e.ocr == nil || e.attachmentService == nil || imageRef.Size <= attachmentapp.MaxVisionImageBytes {
		return ""
	}
	raw, err := e.attachmentService.ReadImageBytes(ctx, imageRef.ID, imageRef.SessionID)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return e.attachedImageOCRText(ctx, []llmadapter.Image{{MIME: imageRef.MIME, Data: raw}})
}

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
	block := "[本机文字识别]\n" + text + "\n请根据这些识别结果继续。用户在问图片是什么时直接回答。后面还有工作时用这份结果接着做，不要再识别图片。"
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

func imageHasFollowUpWork(text string) bool {
	t := chatRoutingText(text)
	if wantsComputerAction(t) {
		return true
	}
	for _, word := range []string{"然后", "之后", "接着", "生成", "发给", "保存", "整理", "根据", "参考"} {
		if strings.Contains(t, word) {
			return true
		}
	}
	return false
}

func wantsComputerAction(text string) bool {
	t := chatRoutingText(text)
	for _, word := range []string{"打开", "点开", "点击", "播放", "执行", "运行", "操作电脑", "帮我点"} {
		if strings.Contains(t, word) {
			return true
		}
	}
	return false
}

func dropComputerTools(tools []llmadapter.ToolDefinition) []llmadapter.ToolDefinition {
	if len(tools) == 0 {
		return tools
	}
	out := make([]llmadapter.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		switch {
		case tool.Name == "command.run", tool.Name == "system.run", tool.Name == "computer.act", tool.Name == "browser.act":
			continue
		case strings.HasPrefix(tool.Name, "cc."), strings.HasPrefix(tool.Name, "desktop."):
			continue
		default:
			out = append(out, tool)
		}
	}
	return out
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
	// OCR model, then the local OCR model, then Windows OCR. The first usable
	// result is handed to this chat turn. A second vision model is not started
	// after that ladder. Pixels stay only when no OCR service ran and this
	// chat model can see images itself.
	if text := e.attachedImageOCRText(ctx, images); text != "" {
		return text, true
	}
	if e != nil && e.ocr != nil {
		return "", false
	}
	if llm.SupportsVision || e.providers == nil {
		return "", false
	}
	if e.providers == nil {
		return "", false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", false
	}
	full := provider.VisionDescribeCatalog(items, llm.ModelID)
	catalog := e.preferBoundCatalog(ctx, "vision", full)
	if len(catalog) == 0 {
		catalog = full
	} else {
		seen := map[string]bool{}
		for _, entry := range catalog {
			seen[entry.Provider.ID+"\x00"+entry.Model.ModelID] = true
		}
		for _, entry := range full {
			if !seen[entry.Provider.ID+"\x00"+entry.Model.ModelID] {
				catalog = append(catalog, entry)
			}
		}
	}
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
