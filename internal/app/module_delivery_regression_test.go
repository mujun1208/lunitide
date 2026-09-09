package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type moduleDeliveryAdapter struct {
	processReplyAdapter
	call  llmadapter.ToolCall
	calls int
}

func (a *moduleDeliveryAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		if err := emit(llmadapter.Delta{ToolCall: &a.call}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{a.call}}, FinishReason: "tool_calls"}, nil
	}
	if a.calls > 2 {
		return llmadapter.Response{}, fmt.Errorf("unexpected repeated finalization")
	}
	const reply = "已生成文件。"
	if err := emit(llmadapter.Delta{Text: reply}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: reply}, FinishReason: "stop"}, nil
}

// Exercise real generators and durable receipts through the shared execution
// engine. Provider responses are deterministic fixtures, not paid model calls.
func TestModuleDocumentDeliveryAcrossSharedEntryContexts(t *testing.T) {
	docx, _ := json.Marshal(map[string]any{"path": "review.docx", "title": "中文交付", "blocks": officetools.SampleStyledDocxBlocks()})
	formats := []struct{ kind, tool, goal, args, part string }{
		{"xlsx", "excel.gen", "生成 Excel 表格", `{"path":"review.xlsx","sheets":[{"name":"中文","headers":["项目","金额"],"rows":[["验收",123]]}]}`, "xl/workbook.xml"},
		{"docx", "docx.gen", "生成 Word 文档", string(docx), "word/document.xml"},
		{"pptx", "pptx.gen", "生成 PPT", `{"path":"review.pptx","title":"中文交付","slides":[{"title":"验收","bullets":["保留中文正文","金额 123"]}]}`, "ppt/slides/slide1.xml"},
		{"pdf", "pdf.gen", "生成 PDF 文档", `{"path":"review.pdf","title":"中文交付","body":"本周完成接口联调。\n金额 123，验收完成。"}`, ""},
	}
	for _, entry := range []string{"typed", "voice", "office-context"} {
		for _, f := range formats {
			t.Run(entry+"/"+f.kind, func(t *testing.T) {
				base, _, sid, _ := messageEngine(t)
				e := NewEngineWithGateway(nil, "test", streamTestLease{})
				e.messages, e.sessions = base.messages, base.sessions
				runtime, err := toolruntime.New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Close()
				e.tools = runtime
				defer e.StopChatMemoryWorkers()
				a := &moduleDeliveryAdapter{call: llmadapter.ToolCall{ID: "deliver", Name: f.tool, Arguments: json.RawMessage(f.args)}}
				e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if entry == "office-context" {
					ctx = withOfficeTask(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAA")
				}
				var events []bridge.Event
				e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", &streamState{cancel: cancel, state: streamRunning, companion: entry == "voice"}, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "fixture"}, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: f.goal + "。按已提供内容生成，不联网，不打开应用。"}}, Tools: engineToolDefinitions()}, func(event bridge.Event) error { events = append(events, event); return nil }, sid, executionModeFullAccess)
				if len(events) == 0 || events[len(events)-1].Type != bridge.EventCompleted || a.calls != 2 {
					t.Fatalf("completion=%+v calls=%d", events, a.calls)
				}
				terminal := events[len(events)-1].Completed
				if terminal == nil || terminal.MessageID == "" || terminal.PersistFailed {
					t.Fatal("completion not persisted")
				}
				artifacts := e.loadSessionArtifactsByMessage(sid)[terminal.MessageID]
				if len(artifacts) != 1 || artifacts[0].Kind != f.kind {
					t.Fatalf("artifacts=%+v", artifacts)
				}
				folder, err := runtime.SessionFolder(sid)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(folder, artifacts[0].Path))
				if err != nil {
					t.Fatal(err)
				}
				if f.kind == "pdf" {
					if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data, []byte("/ToUnicode")) {
						t.Fatal("PDF or embedded Chinese mapping missing")
					}
				} else {
					z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
					if err != nil {
						t.Fatal(err)
					}
					part, err := z.Open(f.part)
					if err != nil {
						t.Fatal(err)
					}
					part.Close()
				}
			})
		}
	}
}

type rejectedImageAdapter struct {
	processReplyAdapter
	calls  int
	sawOCR bool
}

func (a *rejectedImageAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if len(req.Images) > 0 {
		return llmadapter.Response{}, &llmadapter.Error{HTTPStatus: 400, Message: "This model does not support image"}
	}
	a.sawOCR = strings.Contains(lastUserContent(req.Messages), "OCR LINE from catalog")
	const reply = "识别文字已收到。"
	if err := emit(llmadapter.Delta{Text: reply}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: reply}, FinishReason: "stop"}, nil
}

func TestDeclaredVisionModelRejectionDelegatesToConfiguredOCR(t *testing.T) {
	for _, companion := range []bool{false, true} {
		t.Run(fmt.Sprint(companion), func(t *testing.T) {
			e := NewEngineWithGateway(visionCatalogProvider{supportsVision: true}, "test", streamTestLease{})
			a := &rejectedImageAdapter{}
			ocrCalls := 0
			e.SetAdapterFactoryForTest(func(_ context.Context, p provider.Provider) (llmadapter.Adapter, error) {
				if p.ID == visionCatalogProviderID {
					return visionFallbackAdapter{completeCalls: &ocrCalls}, nil
				}
				return a, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer e.StopChatMemoryWorkers()
			p, _ := visionCatalogProvider{}.Get(ctx, chatAttachmentProviderID)
			e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", &streamState{cancel: cancel, state: streamRunning, companion: companion}, p, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "识别图片文字"}}, Images: []llmadapter.Image{{MIME: "image/png", Data: []byte("fixture")}}}, func(bridge.Event) error { return nil }, "", executionModeFullAccess)
			if a.calls != 2 || ocrCalls != 1 || !a.sawOCR {
				t.Fatalf("llm=%d ocr=%d delivered=%v", a.calls, ocrCalls, a.sawOCR)
			}
		})
	}
}
