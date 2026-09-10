package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type catalogImageAdapter struct {
	processReplyAdapter
	out   llmadapter.MediaResult
	calls int
	err   error
}

func (a *catalogImageAdapter) GenerateImage(context.Context, []byte, string, string) (llmadapter.MediaResult, error) {
	a.calls++
	return a.out, a.err
}

func TestCatalogImageFailureDoesNotSubmitAnotherPaidGeneration(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "image-model", Kind: provider.KindImage, KindDefault: true}}
	backup := p
	backup.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	for _, failure := range []error{context.DeadlineExceeded, &llmadapter.Error{Code: "INVALID_RESPONSE"}, &llmadapter.Error{HTTPStatus: 500}} {
		e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p, backup}}, "test", streamTestLease{})
		a := &catalogImageAdapter{err: failure}
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
		_, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", json.RawMessage(`{"prompt":"square"}`))
		if err == nil || a.calls != 1 {
			t.Fatalf("failure=%v err=%v paid submissions=%d", failure, err, a.calls)
		}
	}
}

func TestCatalogImageBytesBecomeAnOpenableArtifactWithoutResubmission(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"generated.png", "../escape.png"} {
		t.Run(path, func(t *testing.T) {
			p := videoTestProvider()
			p.Models = []provider.Model{{ModelID: "image-model", Kind: provider.KindImage, KindDefault: true}}
			e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
			r, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			e.tools = r
			a := &catalogImageAdapter{out: llmadapter.MediaResult{Data: data.Bytes(), MIME: "image/png"}}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
			args, _ := json.Marshal(map[string]string{"prompt": "square", "path": path})
			out, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
			if a.calls != 1 {
				t.Fatalf("paid submissions=%d", a.calls)
			}
			if path == "generated.png" {
				if err != nil || out.Artifact == nil {
					t.Fatalf("lost image: %+v %v", out, err)
				}
			} else if err == nil {
				t.Fatal("unsafe output path accepted")
			}
		})
	}
}

func TestCatalogMediaURLDoesNotClaimLocalFileWasSaved(t *testing.T) {
	p := videoTestProvider()
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	a := &videoDeadlineAdapter{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	out, err := e.invokeMediaGenerate(context.Background(), "", "video.generate", json.RawMessage(`{"prompt":"square","path":"result.mp4"}`))
	if err != nil || !strings.Contains(out.Output, "尚未保存") || !strings.Contains(out.Output, "https://cdn.example/final.mp4") {
		t.Fatalf("%+v %v", out, err)
	}
}

type truncatedVisionAdapter struct{ processReplyAdapter }

type catalogOCRContractAdapter struct {
	processReplyAdapter
	request llmadapter.Request
}

func (a *catalogOCRContractAdapter) Complete(_ context.Context, _ []byte, request llmadapter.Request) (llmadapter.Response, error) {
	a.request = request
	return llmadapter.Response{Message: llmadapter.Message{Content: "123"}, FinishReason: "stop"}, nil
}

func TestMaybeDescribeImagesRecordsVisionPurpose(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "vision-desc", Kind: provider.KindVision, KindDefault: true, SupportsVision: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, content: "发票 12"}, nil
	})
	parent := withContinuityScope(context.Background(), continuityScope{Owner: "sess", Task: "sess", Turn: "turn", Purpose: "chat"})
	text, ok := e.maybeDescribeImages(parent, provider.Model{ModelID: "llm"}, []llmadapter.Image{{MIME: "image/png", Data: []byte("fixture")}}, "这张图是什么")
	if !ok || !strings.Contains(text, "发票") {
		t.Fatalf("vision describe %+v %v", text, ok)
	}
	seen := false
	for _, rec := range store.recs {
		if rec.Purpose == "vision" {
			seen = true
		}
		if rec.Purpose == "chat" {
			t.Fatalf("vision describe reused chat purpose: %+v", rec)
		}
	}
	if !seen {
		t.Fatalf("vision purpose missing: %+v", store.recs)
	}
}

func TestCatalogOCRUsesTheSameModelContractAsItsConnectionProbe(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "deepseek-ocr", Kind: provider.KindVision, KindDefault: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	a := &catalogOCRContractAdapter{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	text, ok := e.maybeDescribeImages(context.Background(), provider.Model{ModelID: "llm"}, []llmadapter.Image{{MIME: "image/png", Data: []byte("fixture")}}, "识别")
	if !ok || text != "123" || len(a.request.Messages) != 2 || a.request.Messages[0].Role != llmadapter.RoleSystem || a.request.Messages[0].Content != visionProbeRequest("deepseek-ocr").Messages[0].Content || len(a.request.Images) != 1 {
		t.Fatalf("OCR runtime/probe contract differs: %+v", a.request)
	}
}

func (truncatedVisionAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{Message: llmadapter.Message{Content: "incomplete OCR"}, FinishReason: "length"}, nil
}

func TestOCRTruncationIsNotReportedAsCompleteRecognition(t *testing.T) {
	e := NewEngineWithGateway(visionCatalogProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return truncatedVisionAdapter{}, nil
	})
	if text, ok := e.maybeDescribeImages(context.Background(), provider.Model{ModelID: "llm"}, []llmadapter.Image{{MIME: "image/png", Data: []byte("fixture")}}, "识别"); ok || text != "" {
		t.Fatal("partial OCR accepted")
	}
}
