package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

func TestChatStartFailsWhenProvidersUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	var streamed int
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","messages":[{"role":"user","content":"你好"}]}`), func(bridge.Event) error {
		streamed++
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired providers: %#v", resp)
	}
	if resp.Error.Message != "模型供应商服务未装配" {
		t.Fatalf("unwired detail: %#v", resp.Error)
	}
	if resp.Error.Retryable {
		t.Fatal("missing install must not be retryable")
	}
	if streamed != 0 {
		t.Fatalf("must not start stream: %d events", streamed)
	}
}

func TestChatStartFailsWhenCatalogEmpty(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	var streamed int
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","messages":[{"role":"user","content":"你好"}]}`), func(bridge.Event) error {
		streamed++
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "CAPABILITY_NOT_READY" {
		t.Fatalf("empty catalog: %#v", resp)
	}
	if resp.Error.Message != "请先配置并启用供应商、凭据和模型" {
		t.Fatalf("empty catalog detail: %#v", resp.Error)
	}
	if resp.Error.Retryable {
		t.Fatal("config gap must not be retryable")
	}
	if streamed != 0 {
		t.Fatalf("must not start stream: %d events", streamed)
	}
}

func TestChatStartMissingModelStillModelNotFound(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"missing-model","messages":[{"role":"user","content":"你好"}]}`), func(bridge.Event) error {
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "MODEL_NOT_FOUND" {
		t.Fatalf("missing model swallowed: %#v", resp)
	}
}

type listFailProvider struct{ providerRepositoryStub }

func (listFailProvider) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return nil, errors.New("catalog busy")
}

func TestChatStartCatalogReadFailureIsRetryable(t *testing.T) {
	e := NewEngine(listFailProvider{}, "test")
	var streamed int
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","messages":[{"role":"user","content":"你好"}]}`), func(bridge.Event) error {
		streamed++
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("catalog read: %#v", resp)
	}
	if !resp.Error.Retryable {
		t.Fatal("transient catalog failure must be retryable")
	}
	if streamed != 0 {
		t.Fatalf("must not start stream: %d events", streamed)
	}
}

func TestCompanionStartFailsWhenProvidersUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	var streamed int
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","companion":true,"messages":[{"role":"user","content":"今晚天气"}]}`), func(bridge.Event) error {
		streamed++
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("companion unwired: %#v", resp)
	}
	if resp.Error.Message != "模型供应商服务未装配" {
		t.Fatalf("companion unwired detail: %#v", resp.Error)
	}
	if streamed != 0 {
		t.Fatalf("must not start stream: %d events", streamed)
	}
}

func TestCompanionStartFailsWhenCatalogEmpty(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	var streamed int
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","companion":true,"messages":[{"role":"user","content":"今晚天气"}]}`), func(bridge.Event) error {
		streamed++
		return nil
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "CAPABILITY_NOT_READY" {
		t.Fatalf("companion empty catalog: %#v", resp)
	}
	if resp.Error.Message != "请先配置并启用供应商、凭据和模型" || resp.Error.Retryable {
		t.Fatalf("companion empty catalog detail: %#v", resp.Error)
	}
	if streamed != 0 {
		t.Fatalf("must not start stream: %d events", streamed)
	}
}
