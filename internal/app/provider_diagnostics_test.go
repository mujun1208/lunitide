package app

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestProviderTestUsesEmbedForEmbeddingKind(t *testing.T) {
	p := provider.Provider{Models: []provider.Model{
		{ModelID: "chat", Kind: provider.KindLLM},
		{ModelID: "bge", Kind: provider.KindEmbedding},
	}}
	if !providerTestUsesEmbed(p, "bge") || providerTestUsesEmbed(p, "chat") {
		t.Fatal("provider.test must Embed only embedding kind")
	}
}

type mediaProbeAdapter struct {
	completeCalls int
	imageCalls    int
	videoCalls    int
	request       llmadapter.Request
	streamText    *string
}

func (a *mediaProbeAdapter) Complete(_ context.Context, _ []byte, request llmadapter.Request) (llmadapter.Response, error) {
	a.completeCalls++
	a.request = request
	return llmadapter.Response{}, nil
}

func TestVisionProbeIncludesSyntheticImageAndOCRPrompt(t *testing.T) {
	a := &mediaProbeAdapter{}
	if err := probeProviderModel(context.Background(), a, nil, provider.Model{ModelID: "deepseek-ocr", Kind: provider.KindVision}); err != nil {
		t.Fatal(err)
	}
	if len(a.request.Images) != 1 || a.request.MaxTokens < 32 || a.request.Messages[0].Content != "<image>\nFree OCR." {
		t.Fatalf("invalid OCR probe shape")
	}
	img, err := png.Decode(bytes.NewReader(a.request.Images[0].Data))
	if err != nil || img.Bounds().Dx() != 256 {
		t.Fatal("invalid probe image", err)
	}
}

func TestProviderDiagnosticsExplainFailuresWithoutUpstreamSecrets(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422, 429, 500, 503} {
		d := diagnosticResult(&llmadapter.Error{HTTPStatus: status, Stage: llmadapter.StageHTTP}, 0, time.Now())
		if d.SanitizedMessage == "供应商连接测试失败" || d.HTTPStatus != status {
			t.Fatalf("generic diagnostic: %+v", d)
		}
	}
}

func TestProviderDiagnosticsIdentifyUnavailableModelChannel(t *testing.T) {
	d := diagnosticResult(&llmadapter.Error{Code: "MODEL_CHANNEL_UNAVAILABLE", HTTPStatus: 503, Stage: llmadapter.StageHTTP, Message: "private upstream detail"}, 0, time.Now())
	if d.Retryable || !strings.Contains(d.SanitizedMessage, "可用通道") || strings.Contains(d.SanitizedMessage, "private") {
		t.Fatalf("invalid channel diagnosis: %+v", d)
	}
}
func (a *mediaProbeAdapter) Stream(_ context.Context, _ []byte, request llmadapter.Request, _ func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.request = request
	text := "123"
	if a.streamText != nil {
		text = *a.streamText
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: text}}, nil
}

func TestOCRProbeRejectsSuccessfulButIncorrectRecognition(t *testing.T) {
	for _, value := range []string{"", "124", "connection successful"} {
		a := &mediaProbeAdapter{streamText: &value}
		if err := probeProviderModel(context.Background(), a, nil, provider.Model{ModelID: "deepseek-ocr", Kind: provider.KindVision}); err == nil {
			t.Fatalf("accepted incorrect recognition %q", value)
		}
	}
}
func (*mediaProbeAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a *mediaProbeAdapter) GenerateImage(context.Context, []byte, string, string) (llmadapter.MediaResult, error) {
	a.imageCalls++
	return llmadapter.MediaResult{URL: "https://example.test/probe.png"}, nil
}
func (a *mediaProbeAdapter) GenerateVideo(context.Context, []byte, string, string) (llmadapter.MediaResult, error) {
	a.videoCalls++
	return llmadapter.MediaResult{URL: "https://example.test/probe.mp4"}, nil
}

func TestProviderTestUsesSelectedMediaModelCapability(t *testing.T) {
	a := &mediaProbeAdapter{}
	if err := probeProviderModel(context.Background(), a, nil, provider.Model{ModelID: "seedream", Kind: provider.KindImage}); err != nil {
		t.Fatal(err)
	}
	if err := probeProviderModel(context.Background(), a, nil, provider.Model{ModelID: "seedance", Kind: provider.KindVideo}); err != nil {
		t.Fatal(err)
	}
	if a.imageCalls != 1 || a.videoCalls != 1 || a.completeCalls != 0 {
		t.Fatalf("wrong probe route: %#v", a)
	}
}

func TestProviderAdapterBaseURLAcceptsStoredFullMediaEndpoints(t *testing.T) {
	cases := map[string]string{
		"https://z.apiyihe.org/v1/images/generations":              "https://z.apiyihe.org/v1",
		"https://z.apiyihe.org/volc/v1/contents/generations/tasks": "https://z.apiyihe.org/volc/v1",
		"https://example.test/v1":                                  "https://example.test/v1",
		"http://127.0.0.1:1234/v1/videos/generations/":             "http://127.0.0.1:1234/v1",
	}
	for input, want := range cases {
		if got := providerAdapterBaseURL(input); got != want {
			t.Fatalf("providerAdapterBaseURL(%q)=%q want %q", input, got, want)
		}
	}
}

func TestEmbedKBTextsSkipsAnthropic(t *testing.T) {
	e := NewEngine(anthropicEmbedCatalog{}, "test")
	if _, err := e.embedKBTexts(context.Background(), []string{"landing gear"}); err == nil {
		t.Fatal("D-E4 anthropic embed catalog must skip write")
	}
}

type anthropicEmbedCatalog struct{ providerRepositoryStub }

func (anthropicEmbedCatalog) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAN", Name: "Claude", Protocol: provider.ProtocolAnthropic,
		BaseURL: "https://api.anthropic.com", CredentialRef: "cred",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "claude-embed", Kind: provider.KindEmbedding, KindDefault: true, IsDefault: true}},
	}}, nil
}

func TestDiscoveredModelsPreservesDefaultDeduplicatesAndBounds(t *testing.T) {
	current := provider.Provider{Models: []provider.Model{{ModelID: "m25", DisplayName: "old", IsDefault: true}}}
	input := llmadapter.Discovery{}
	for i := 59; i >= 0; i-- {
		id := "m" + twoDigits(i)
		input.Models = append(input.Models, llmadapter.Model{ID: id})
	}
	input.Models = append(input.Models, llmadapter.Model{ID: "m25"}, llmadapter.Model{ID: " bad "}, llmadapter.Model{ID: ""})
	models, warning, ok := discoveredModels(current, input)
	if !ok || warning != "" || len(models) != 50 {
		t.Fatalf("models=%d warning=%q ok=%v", len(models), warning, ok)
	}
	defaults := 0
	for _, m := range models {
		if m.IsDefault {
			defaults++
			if m.ModelID != "m25" {
				t.Fatalf("default changed to %q", m.ModelID)
			}
		}
	}
	if defaults != 1 || models[0].ModelID != "m00" || models[49].ModelID != "m49" {
		t.Fatalf("non-deterministic result: %#v", models)
	}
}

func TestDiscoveredModelsSelectsDeterministicDefaultAndAnthropicPreserves(t *testing.T) {
	current := provider.Provider{Models: []provider.Model{{ModelID: "old", DisplayName: "Old", IsDefault: true}}}
	models, _, ok := discoveredModels(current, llmadapter.Discovery{Models: []llmadapter.Model{{ID: "z"}, {ID: "a"}}})
	if !ok || !models[0].IsDefault || models[0].ModelID != "a" {
		t.Fatalf("models=%#v", models)
	}
	models, warning, ok := discoveredModels(current, llmadapter.Discovery{Unsupported: true, Warning: "upstream free text must not cross"})
	if !ok || warning != "MODEL_DISCOVERY_UNSUPPORTED" || len(models) != 1 || !models[0].IsDefault {
		t.Fatalf("unsupported result=%#v %q", models, warning)
	}
}

func TestDiagnosticResultIsStableAndContainsNoUpstreamText(t *testing.T) {
	canary := "SECRET-CANARY upstream says no"
	d := diagnosticResult(&llmadapter.Error{Code: "HTTP_401", Stage: llmadapter.StageHTTP, HTTPStatus: 401, Message: canary}, time.Millisecond, time.Unix(1, 0).UTC())
	if d.Status != "failed" || d.Stage != "authenticate" || d.HTTPStatus != 401 || d.ErrorCode != "HTTP_401" || d.SanitizedMessage == canary || d.Retryable {
		t.Fatalf("unsafe diagnostic: %#v", d)
	}
	d = diagnosticResult(errors.New(canary), 0, time.Unix(1, 0).UTC())
	if strings.Contains(d.SanitizedMessage, canary) || d.Stage != "resolve" || d.ErrorCode != "INTERNAL_ERROR" {
		t.Fatalf("unsafe generic diagnostic: %#v", d)
	}
	d = diagnosticResult(context.DeadlineExceeded, 0, time.Unix(1, 0).UTC())
	if d.Stage != "connect" || d.ErrorCode != "TIMEOUT" || !d.Retryable {
		t.Fatalf("timeout diagnostic: %#v", d)
	}
	d = diagnosticResult(context.Canceled, 0, time.Unix(1, 0).UTC())
	if d.Stage != "connect" || d.ErrorCode != "CANCELLED" || d.Retryable {
		t.Fatalf("cancelled diagnostic: %#v", d)
	}
}

func twoDigits(i int) string { return string([]byte{'0' + byte(i/10), '0' + byte(i%10)}) }
