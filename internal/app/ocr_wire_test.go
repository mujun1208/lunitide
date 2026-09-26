package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestOCRProviderCallRecordsOCRPurpose(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "ocr-v1", Kind: provider.KindVision, KindDefault: true, SupportsVision: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, content: "发票 88"}, nil
	})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(ocrapp.Routing{ProviderID: p.ID, ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	parent := withContinuityScope(context.Background(), continuityScope{Owner: "sess", Task: "sess", Turn: "turn", Purpose: "chat"})
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	text, err := e.ocrProviderCall(parent, png, "image-ocr")
	if err != nil || !strings.Contains(text, "发票") {
		t.Fatalf("ocr provider %+v %v", text, err)
	}
	seen := false
	for _, rec := range store.recs {
		if rec.Purpose == "ocr" {
			seen = true
		}
		if rec.Purpose == "chat" {
			t.Fatalf("OCR call reused chat purpose: %+v", rec)
		}
	}
	if !seen {
		t.Fatalf("ocr purpose missing: %+v", store.recs)
	}
}

func TestWorkspaceDocumentTextImageWithoutLocalUsesChinese(t *testing.T) {
	e := NewEngine(nil, "test")
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetLocalImage(nil)
	e.SetOCR(svc)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	_, _, _, _, err := e.workspaceDocumentText(context.Background(), "shot.png", png, "image/png")
	if err == nil || !strings.Contains(err.Error(), "本地图片识别未装配") {
		t.Fatalf("workspace image OCR gap must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "not packaged") {
		t.Fatalf("must not leak English image-OCR gap: %v", err)
	}
}

func TestOCRProviderCallUsesCapabilityVisionWhenOCRRoutingUnbound(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "ocr-v1", Kind: provider.KindVision, KindDefault: true, SupportsVision: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, content: "发票 88"}, nil
	})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{{
		Role: "vision", ProviderID: p.ID, ModelID: "ocr-v1",
	}}})
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	text, err := e.ocrProviderCall(context.Background(), png, "image-ocr")
	if err != nil || !strings.Contains(text, "发票") {
		t.Fatalf("vision role must satisfy OCR without OCR routing bind: %q %v", text, err)
	}
	got, recErr := svc.RecognizeImage(context.Background(), png)
	if recErr != nil || got.Source != ocrapp.SourceProvider || !strings.Contains(got.Text, "发票") {
		t.Fatalf("recognize must use vision first: %+v %v", got, recErr)
	}
}

func TestOCRProviderCallIgnoresStoredNeverRoutingWhenVisionBound(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "ocr-v1", Kind: provider.KindVision, KindDefault: true, SupportsVision: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return completeMeterAdapter{usage: llmadapter.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}, content: "发票 88"}, nil
	})
	path := filepath.Join(t.TempDir(), "ocr-routing.json")
	if err := os.WriteFile(path, []byte(`{"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FZZ","modelId":"stale-ocr","policy":{"mode":"auto","complexDocumentEngine":"paddleocr-vl-1.6","fallbackOrder":["ppocr","windows-ocr"],"sendToCloud":"never"},"revision":"legacy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	svc := ocrapp.New(ocrapp.NewFileStore(path))
	e.SetOCR(svc)
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{{
		Role: "vision", ProviderID: p.ID, ModelID: "ocr-v1",
	}}})
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	text, err := e.ocrProviderCall(context.Background(), png, "image-ocr")
	if err != nil || !strings.Contains(text, "发票") {
		t.Fatalf("stored never routing must not block vision: %q %v", text, err)
	}
	got, recErr := svc.RecognizeImage(context.Background(), png)
	if recErr != nil || got.Source != ocrapp.SourceProvider || !strings.Contains(got.Text, "发票") {
		t.Fatalf("recognize must skip leftover never routing: %+v %v", got, recErr)
	}
}

func TestOCRProviderCallAsksForThePictureWhenThereIsNoText(t *testing.T) {
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "ocr-v1", Kind: provider.KindVision, KindDefault: true, SupportsVision: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	var prompt string
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return promptCaptureAdapter{prompt: &prompt, content: "一只戴红蝴蝶结的白猫"}, nil
	})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(ocrapp.Routing{ProviderID: p.ID, ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := e.ocrProviderCall(context.Background(), png, "image-ocr"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Do not describe") || !strings.Contains(prompt, "这张图是什么") {
		t.Fatal(prompt)
	}
}

type promptCaptureAdapter struct {
	prompt  *string
	content string
}

func (a promptCaptureAdapter) Complete(_ context.Context, _ []byte, req llmadapter.Request) (llmadapter.Response, error) {
	if a.prompt != nil && len(req.Messages) > 0 {
		*a.prompt = req.Messages[0].Content
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: a.content}}, nil
}
func (promptCaptureAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (promptCaptureAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}

func TestOCRProviderCallUnboundUsesChinese(t *testing.T) {
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{videoTestProvider()}}, "test", streamTestLease{})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	_, err := e.ocrProviderCall(context.Background(), png, "image-ocr")
	if err == nil || !strings.Contains(err.Error(), "OCR 路由未绑定") {
		t.Fatalf("unbound provider OCR must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "ocr routing unbound") {
		t.Fatalf("must not leak English provider-OCR gap: %v", err)
	}
}

func TestOCRProviderCallRawPDFUsesChinese(t *testing.T) {
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{videoTestProvider()}}, "test", streamTestLease{})
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	e.SetOCR(svc)
	_, err := e.ocrProviderCall(context.Background(), []byte("%PDF-1.4 scanned"), "pdf-ocr")
	if err == nil || !strings.Contains(err.Error(), "本地分页识别") {
		t.Fatalf("raw PDF provider OCR must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "raw PDF bytes") {
		t.Fatalf("must not leak English raw-PDF provider gap: %v", err)
	}
}

func TestOCRProviderCallUnavailableUsesChinese(t *testing.T) {
	e := NewEngine(nil, "test")
	_, err := e.ocrProviderCall(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image-ocr")
	if err == nil || !strings.Contains(err.Error(), "OCR 供应商不可用") {
		t.Fatalf("unwired provider OCR must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "ocr provider unavailable") {
		t.Fatalf("must not leak English provider-unavailable gap: %v", err)
	}
}
