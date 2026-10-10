package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/memory"
)

// capability-self-bootstrap P4: an approved preset install settles into
// semantic memory (upsert, never duplicate), survives nil services, and the
// persisted preset map keeps "previously approved" findable for discovery.

func TestRecordApprovedMcpPresetWritesSemanticMemory(t *testing.T) {
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	e.recordApprovedMcpPreset(context.Background(), sessionID, "fetch", "connected")
	if len(store.items) != 1 {
		t.Fatalf("want 1 memory, got %#v", store.items)
	}
	m := store.items[0]
	if m.Layer != memory.LayerSemantic || m.Scope != memory.ScopeProject {
		t.Fatalf("layer/scope = %v/%v", m.Layer, m.Scope)
	}
	if m.Key != "mcp-preset-approval:fetch" {
		t.Fatalf("key = %q", m.Key)
	}
	if !strings.Contains(m.Content, "Fetch") || !strings.Contains(m.Content, "用户批准") {
		t.Fatalf("content = %q", m.Content)
	}
	e.recordApprovedMcpPreset(context.Background(), sessionID, "fetch", "needs_configuration")
	if len(store.items) != 1 {
		t.Fatal("upsert must not duplicate")
	}
	if !strings.Contains(store.items[0].Content, "配置密钥") {
		t.Fatalf("state refresh missed: %q", store.items[0].Content)
	}
}

func TestRecordApprovedMcpPresetFailsClosed(t *testing.T) {
	e := NewEngine(nil, "test")
	// No memory service, no session mapping, unknown preset: all must be
	// silent no-ops, never panics or errors.
	e.recordApprovedMcpPreset(context.Background(), "s", "fetch", "connected")
	e.sessions = sessionGetStub{projectID: "01ARZ3NDEKTSV4RRFFQ69G5FAY"}
	e.recordApprovedMcpPreset(context.Background(), "s", "no-such-preset", "connected")
}

func TestPresetInstalledBeforeUsesPersistMap(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if e.presetInstalledBefore("fetch") {
		t.Fatal("clean engine must not report installs")
	}
	e.mcpPresetByEP.Store("ep-1", "fetch")
	if !e.presetInstalledBefore("fetch") {
		t.Fatal("persist map hit must report previous install")
	}
	if e.presetInstalledBefore("playwright") {
		t.Fatal("other presets stay uninstall")
	}
	if _, live := e.installedPresetEndpoint("fetch"); live {
		t.Fatal("nil registry must fail closed (fresh install proceeds)")
	}
}
