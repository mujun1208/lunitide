package app

import (
	"context"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestResolveChatModelPrefersRemembered(t *testing.T) {
	first := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-a",
		CreatedAt: time.Unix(1, 0).UTC(),
		Models:    []provider.Model{{ModelID: "first-model", Kind: provider.KindLLM, KindDefault: true}},
	}
	second := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-b",
		CreatedAt: time.Unix(2, 0).UTC(),
		Models:    []provider.Model{{ModelID: "second-model", Kind: provider.KindLLM}},
	}
	items := []provider.Provider{first, second}
	got, ok := resolveChatModel(items, "", "")
	if !ok || got.Model.ModelID != "first-model" {
		t.Fatalf("default = %+v ok=%v", got, ok)
	}
	got, ok = resolveChatModel(items, second.ID, "second-model")
	if !ok || got.Provider.ID != second.ID || got.Model.ModelID != "second-model" {
		t.Fatalf("remembered = %+v ok=%v", got, ok)
	}
	e := NewEngine(nil, "test")
	e.rememberChatModel(second.ID, "second-model")
	got, ok = e.resolvePreferredChatModel(items)
	if !ok || got.Model.ModelID != "second-model" {
		t.Fatalf("engine preferred = %+v ok=%v", got, ok)
	}
}

func TestRememberedChatModelSurvivesEngineRestart(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	first := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-a",
		CreatedAt: time.Unix(1, 0).UTC(),
		Models:    []provider.Model{{ModelID: "first-model", Kind: provider.KindLLM, KindDefault: true}},
	}
	second := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-b",
		CreatedAt: time.Unix(2, 0).UTC(),
		Models:    []provider.Model{{ModelID: "second-model", Kind: provider.KindLLM}},
	}
	items := []provider.Provider{first, second}
	writer := NewEngine(nil, "test")
	writer.SetToolRuntime(tools)
	writer.rememberChatModel(second.ID, "second-model")
	reader := NewEngine(nil, "test")
	reader.SetToolRuntime(tools)
	got, ok := reader.resolvePreferredChatModel(items)
	if !ok || got.Provider.ID != second.ID || got.Model.ModelID != "second-model" {
		t.Fatalf("restart preferred = %+v ok=%v", got, ok)
	}
}

func TestHandleChatPreferRemembersWithoutChatStart(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	first := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-a",
		CreatedAt: time.Unix(1, 0).UTC(),
		Models:    []provider.Model{{ModelID: "first-model", Kind: provider.KindLLM, KindDefault: true}},
	}
	second := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Status: provider.StatusEnabled,
		CredentialState: provider.CredentialConfigured, CredentialRef: "ref-b",
		CreatedAt: time.Unix(2, 0).UTC(),
		Models:    []provider.Model{{ModelID: "second-model", Kind: provider.KindLLM}},
	}
	writer := NewEngine(nil, "test")
	writer.SetToolRuntime(tools)
	resp := writer.Handle(context.Background(), validRequest("chat.prefer", `{"providerId":"`+second.ID+`","modelId":"second-model"}`))
	if !resp.OK {
		t.Fatalf("prefer: %#v", resp)
	}
	reader := NewEngine(nil, "test")
	reader.SetToolRuntime(tools)
	got, ok := reader.resolvePreferredChatModel([]provider.Provider{first, second})
	if !ok || got.Provider.ID != second.ID || got.Model.ModelID != "second-model" {
		t.Fatalf("preferred after prefer = %+v ok=%v", got, ok)
	}
}
