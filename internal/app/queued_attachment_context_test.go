package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/ocrapp"
)

// 排队补充里的 [attachment:ID|名称] token 必须展开为真实附件内容，
// 否则模型只看到裸 token，会把已上传的附件当成“还没发”。
func TestQueuedAttachmentContextExpandsTokens(t *testing.T) {
	notes := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		OriginalName: "notes.txt", MIME: "text/plain", ParseStatus: attachment.StatusSucceeded,
		ParsedText: "QUEUE TEXT CONTENT",
	}
	other := attachment.Attachment{
		ID: chatAttachmentOtherID, ProjectID: chatAttachmentProjectID, SessionID: "01ARZ3NDEKTSV4RRFFQQQQQQQQ",
		OriginalName: "other.png", MIME: "image/png", ParseStatus: attachment.StatusSucceeded,
		ParsedText: "OTHER SESSION IMAGE OCR",
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{notes.ID: &notes, other.ID: &other}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, nil, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, nil))

	req := &llmadapter.Request{}
	texts := []string{
		"[attachment:" + chatAttachmentID + "|notes.txt] 这条是文本附件",
		"无 token 的普通补充",
		"[attachment:" + chatAttachmentOtherID + "|other.png] 别的会话的附件",
		"[attachment:01ARZ3NDEKTSV4RRFFQQQQQQQR|missing.png] 已被删掉的附件",
	}
	got := e.queuedAttachmentContext(context.Background(), chatAttachmentSessionID, texts, req, provider.Provider{})

	if !strings.Contains(got, "notes.txt") || !strings.Contains(got, "QUEUE TEXT CONTENT") {
		t.Fatalf("text attachment excerpt missing: %q", got)
	}
	if strings.Contains(got, "OTHER SESSION") {
		t.Fatalf("cross-session attachment leaked: %q", got)
	}
	if strings.Contains(got, "missing.png") {
		t.Fatalf("deleted attachment should be skipped: %q", got)
	}
}

// 附件图片在排队路径优先以画面进入多模态请求；画面不可用时回退 OCR 文本。
func TestQueuedAttachmentContextImageFallback(t *testing.T) {
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		FileRef: "queued-image", OriginalName: "queued.png", MIME: "image/png", Size: 9,
		ParseStatus: attachment.StatusSucceeded, ParsedText: "LUNITIDE OCR 8888",
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, nil, nil, "test", streamTestLease{})
	// 无文件存储 → GetVisionImage 失败 → OCR 文本兜底。
	e.SetAttachmentService(attachmentapp.NewService(store, nil))

	req := &llmadapter.Request{}
	got := e.queuedAttachmentContext(context.Background(), chatAttachmentSessionID, []string{"[attachment:" + chatAttachmentID + "|queued.png] 图里写了什么"}, req, provider.Provider{})
	if !strings.Contains(got, "queued.png") || !strings.Contains(got, "LUNITIDE OCR 8888") {
		t.Fatalf("image OCR fallback missing: %q", got)
	}
	if len(req.Images) != 0 {
		t.Fatalf("no vision storage should mean no injected pixels: %#v", req.Images)
	}
}

// 用户链路约束：图片先走 OCR 链（供应商配置的 OCR 模型 → 本地下载的 OCR 模型 →
// Windows OCR 兜底），读出文字就以文字进上下文——即使当前聊天模型支持看图，
// 像素也不发给它。排队路径与 chat.start 正常路径行为一致。
func TestQueuedAttachmentContextOCRLadderBeforePixels(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	digest := sha256.Sum256(data)
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		FileRef: "queued-shot", OriginalName: "shot.png", MIME: "image/png", Size: int64(len(data)),
		SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed,
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, nil, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, chatAttachmentFiles{"queued-shot": data}))
	ocr := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	ocr.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "local-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "LUNITIDE OCR 8888"}}}, nil
	})
	e.SetOCR(ocr)

	// chatAttachmentProvider 的 model SupportsVision=true，但 OCR 已读出文字，
	// 像素必须被替换为文字，不允许发给聊天模型。
	prov, err := chatAttachmentProvider{}.Get(context.Background(), chatAttachmentProviderID)
	if err != nil {
		t.Fatalf("provider fixture: %v", err)
	}
	req := &llmadapter.Request{Model: "model"}
	got := e.queuedAttachmentContext(context.Background(), chatAttachmentSessionID, []string{"[attachment:" + chatAttachmentID + "|shot.png] 图里写了什么"}, req, prov)
	if !strings.Contains(got, "LUNITIDE OCR 8888") || !strings.Contains(got, "[本机文字识别]") {
		t.Fatalf("OCR ladder text missing: %q", got)
	}
	if len(req.Images) != 0 {
		t.Fatalf("OCR text must replace pixels even for vision models: %#v", req.Images)
	}
}

// 纯文本模型（SupportsVision=false）在 OCR 全空时绝不收到像素，
// 而是得到诚实的识别失败说明——镜像 chat.start 的 else-if 分支。
func TestQueuedAttachmentContextTextModelNeverReceivesPixels(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	digest := sha256.Sum256(data)
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		FileRef: "queued-shot", OriginalName: "shot.png", MIME: "image/png", Size: int64(len(data)),
		SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed,
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, nil, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, chatAttachmentFiles{"queued-shot": data}))
	// 无 OCR 服务、无 ParsedText、模型不支持看图：像素必须丢弃，给诚实提示。

	req := &llmadapter.Request{Model: "text-only"}
	got := e.queuedAttachmentContext(context.Background(), chatAttachmentSessionID, []string{"[attachment:" + chatAttachmentID + "|shot.png] 图里写了什么"}, req, provider.Provider{})
	if !strings.Contains(got, "没有读出内容") {
		t.Fatalf("honest failure note missing: %q", got)
	}
	if len(req.Images) != 0 {
		t.Fatalf("text-only model must never receive pixels: %#v", req.Images)
	}
}
