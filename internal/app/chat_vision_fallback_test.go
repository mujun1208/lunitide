package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
)

const visionCatalogProviderID = "01ARZ3NDEKTSV4RRFFQ69G5FBA"

type visionCatalogProvider struct {
	chatAttachmentProvider
	supportsVision bool
}

func (p visionCatalogProvider) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{
		ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com", CredentialRef: "credential-ref",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "model", DisplayName: "Chat", IsDefault: true, SupportsVision: p.supportsVision}},
	}, nil
}

func (visionCatalogProvider) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{{
		ID: visionCatalogProviderID, Name: "Vision", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://vision.example", CredentialRef: "vision-ref",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "ocr", DisplayName: "OCR", IsDefault: true, Kind: provider.KindVision, KindDefault: true}},
	}}, nil
}

type visionFallbackAdapter struct {
	id            string
	requests      chan gateway.Request
	completeCalls *int
	completeImgs  *[]gateway.Image
}

func (a visionFallbackAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	if a.completeCalls != nil {
		*a.completeCalls++
	}
	if a.completeImgs != nil {
		*a.completeImgs = append([]gateway.Image(nil), req.Images...)
	}
	return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: "OCR LINE from catalog"}}, nil
}
func (a visionFallbackAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}
func (a visionFallbackAdapter) Stream(_ context.Context, _ []byte, req gateway.Request, _ func(gateway.Delta) error) (gateway.Response, error) {
	a.requests <- req
	return gateway.Response{}, nil
}

func startVisionFallbackChat(t *testing.T, supportsVision bool) (bridge.Response, gateway.Request, int, []gateway.Image) {
	t.Helper()
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	digest := sha256.Sum256(data)
	image := attachment.Attachment{
		ID: chatAttachmentID, ProjectID: chatAttachmentProjectID, SessionID: chatAttachmentSessionID,
		FileRef: "selected-image", OriginalName: "selected.png", MIME: "image/png", Size: int64(len(data)),
		SHA256: hex.EncodeToString(digest[:]), ParseStatus: attachment.StatusFailed,
	}
	store := &chatAttachmentStore{byID: map[string]*attachment.Attachment{image.ID: &image}}
	requests := make(chan gateway.Request, 1)
	completeCalls := 0
	var completeImgs []gateway.Image
	e := NewEngineWithContextReader(visionCatalogProvider{supportsVision: supportsVision}, nil, nil, nil, chatAttachmentReader{}, nil, "test", streamTestLease{})
	e.SetAttachmentService(attachmentapp.NewService(store, chatAttachmentFiles{image.FileRef: data}))
	e.SetAdapterFactoryForTest(func(_ context.Context, p provider.Provider) (gateway.Adapter, error) {
		return visionFallbackAdapter{id: p.ID, requests: requests, completeCalls: &completeCalls, completeImgs: &completeImgs}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"current question"}],"contextRefs":[{"type":"attachment","id":"` + image.ID + `"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	req := capturedChatRequest(t, requests)
	return response, req, completeCalls, completeImgs
}

func TestChatStartVisionFallbackWhenLLMLacksVision(t *testing.T) {
	response, req, completeCalls, completeImgs := startVisionFallbackChat(t, false)
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	if completeCalls != 1 || len(completeImgs) != 1 {
		t.Fatalf("vision complete calls=%d images=%d", completeCalls, len(completeImgs))
	}
	if len(req.Images) != 0 {
		t.Fatalf("LLM should not receive images after vision fallback: %#v", req.Images)
	}
	var combined strings.Builder
	for _, message := range req.Messages {
		combined.WriteString(message.Content)
	}
	if !strings.Contains(combined.String(), "OCR LINE from catalog") || !strings.Contains(combined.String(), "[视觉模型识别]") {
		t.Fatalf("vision description missing: %q", combined.String())
	}
}

func TestChatStartKeepsImagesWhenLLMSupportsVision(t *testing.T) {
	response, req, completeCalls, _ := startVisionFallbackChat(t, true)
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	if completeCalls != 0 {
		t.Fatalf("vision catalog must not run when LLM supports vision, calls=%d", completeCalls)
	}
	if len(req.Images) != 1 {
		t.Fatalf("LLM-only vision path lost images: %#v", req.Images)
	}
}
