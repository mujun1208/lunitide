package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestCheckpointKeepsUnknownFieldsAndDoesNotClaimNative(t *testing.T) {
	raw := []byte(`{"status":"interrupted","goal":"写周报","streamId":"01ARZ3NDEKTSV4RRFFQ69G5FAA","lastTools":["workspace.read"],"updatedAt":"2026-09-09T00:00:00Z","s2Private":{"keep":true}}`)
	var cp chatTurnCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.Goal != "写周报" {
		t.Fatalf("goal lost: %#v", cp)
	}
	out, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["s2Private"]) != `{"keep":true}` {
		t.Fatalf("unknown field dropped: %s", out)
	}
	if got := completenessOf(cp); got != modelfit.CompletenessStructuredOnly {
		t.Fatalf("structured checkpoint claimed %q", got)
	}
}

func TestLegacyCheckpointStaysLegacyUnknown(t *testing.T) {
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String(), Status: turnStatusInterrupted}
	if got := completenessOf(cp); got != modelfit.CompletenessLegacyUnknown {
		t.Fatalf("empty legacy claimed %q", got)
	}
}

func TestSeedContinuationFromScopeKeepsDispatchIdentity(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "生成日报", StreamID: ulid.Make().String()}
	ctx := withAutomationDispatch(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAR", "job:01ARZ3NDEKTSV4RRFFQ69G5FAR")
	seedContinuationFromScope(ctx, &cp)
	attachContinuation("s1", "test", &cp)
	if cp.Continuation == nil || cp.Continuation.AutomationRunID != "01ARZ3NDEKTSV4RRFFQ69G5FAR" || cp.Continuation.DispatchKey != "job:01ARZ3NDEKTSV4RRFFQ69G5FAR" {
		t.Fatalf("dispatch identity lost: %#v", cp.Continuation)
	}
	if cp.Continuation.Completeness == modelfit.CompletenessNativeComplete {
		t.Fatal("S1 must not claim native_complete")
	}
	seedContinuationFromScope(context.Background(), &cp)
	if cp.Continuation.AutomationRunID != "01ARZ3NDEKTSV4RRFFQ69G5FAR" {
		t.Fatal("empty scope must not erase stored dispatch identity")
	}
}

func TestAttachObtainedProtocolSavesProviderReasoningOnly(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String(), LastTools: []string{"workspace.read"}}
	attachObtainedProtocol(&cp, "private chain")
	attachContinuation("s1", "test", &cp)
	if cp.Continuation == nil || cp.Continuation.Protocol.ReasoningContent != "private chain" || cp.Continuation.Protocol.Source != modelfit.SourceProvider {
		t.Fatalf("provider reasoning not kept: %#v", cp.Continuation)
	}
	if cp.Continuation.Completeness != modelfit.CompletenessStructuredOnly {
		t.Fatalf("S1 must not claim native_complete: %q", cp.Continuation.Completeness)
	}
	attachObtainedProtocol(&cp, "")
	if cp.Continuation.Protocol.ReasoningContent != "private chain" {
		t.Fatal("empty obtain must not erase stored protocol")
	}
	blank := chatTurnCheckpoint{Goal: "hi"}
	attachObtainedProtocol(&blank, "   ")
	if blank.Continuation != nil && blank.Continuation.Protocol.ReasoningContent != "" {
		t.Fatalf("whitespace must not become protocol state: %#v", blank.Continuation)
	}
	if got := obtainedProtocolReasoning(llmadapter.Response{Reasoning: "from adapter"}, "streamed", true); got != "from adapter" {
		t.Fatalf("adapter reasoning is obtained even when UI thinking is off: %q", got)
	}
	if got := obtainedProtocolReasoning(llmadapter.Response{}, "guessed from logs", true); got != "" {
		t.Fatalf("disabled thinking without adapter field must not invent protocol: %q", got)
	}
}

func TestInterruptedToolLoopCheckpointKeepsObtainedProtocol(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	turn := chatTurnCheckpoint{Goal: "写周报", LastTools: []string{"workspace.read"}}
	if err := e.saveTurnCheckpointAfterModel(sessionID, &turn, llmadapter.Response{Reasoning: "obtained chain"}, "ui thinking", false, nil); err != nil {
		t.Fatal(err)
	}
	if turn.Continuation == nil || turn.Continuation.Protocol.ReasoningContent != "obtained chain" {
		t.Fatalf("mid-loop checkpoint must keep obtained protocol in memory: %#v", turn.Continuation)
	}
	got := e.loadTurnCheckpoint(sessionID)
	if got.Continuation == nil || got.Continuation.Protocol.ReasoningContent != "obtained chain" {
		t.Fatalf("interrupted tool-loop checkpoint dropped obtained protocol: %#v", got.Continuation)
	}
	if got.Continuation.Completeness == modelfit.CompletenessNativeComplete {
		t.Fatal("S1 must not claim native_complete on obtained protocol")
	}
}

func TestPinContinuationIdentityFillsOnceAndKeepsFoldGuard(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String(), LastTools: []string{"workspace.read"}}
	pinContinuationIdentity(&cp, continuationIdentity{
		ProviderDeploymentRef: "prov-1",
		ModelRequested:        "deepseek-chat",
		RuntimeEpoch:          "epoch-1",
		OwnerScope:            "session-1",
		TaskRef:               "task-1",
		OperationRefs:         []string{"01ARZ3NDEKTSV4RRFFQ69G5FAA"},
	})
	attachContinuation("s1", "test", &cp)
	env := cp.Continuation
	if env == nil {
		t.Fatal("expected continuation envelope")
	}
	if env.ProviderDeploymentRef != "prov-1" || env.ModelRequested != "deepseek-chat" || env.RuntimeEpoch != "epoch-1" {
		t.Fatalf("identity not pinned: %#v", env)
	}
	if env.OwnerScope != "session-1" || env.TaskRef != "task-1" {
		t.Fatalf("scope not pinned: %#v", env)
	}
	if len(env.OperationRefs) != 1 || env.OperationRefs[0] != "01ARZ3NDEKTSV4RRFFQ69G5FAA" {
		t.Fatalf("operation refs: %#v", env.OperationRefs)
	}
	if env.Completeness == modelfit.CompletenessNativeComplete {
		t.Fatal("S1 pin must not claim native_complete")
	}

	pinContinuationIdentity(&cp, continuationIdentity{
		ProviderDeploymentRef: "prov-2",
		ModelRequested:        "glm-4",
		RuntimeEpoch:          "epoch-2",
		OwnerScope:            "other",
		TaskRef:               "other-task",
		OperationRefs:         []string{"01ARZ3NDEKTSV4RRFFQ69G5FAA", "01ARZ3NDEKTSV4RRFFQ69G5FAB"},
	})
	if env.ProviderDeploymentRef != "prov-1" || env.ModelRequested != "deepseek-chat" || env.RuntimeEpoch != "epoch-1" {
		t.Fatalf("later settings must not rewrite pinned identity: %#v", env)
	}
	if env.OwnerScope != "session-1" || env.TaskRef != "task-1" {
		t.Fatalf("later settings rewrote scope: %#v", env)
	}
	if len(env.OperationRefs) != 2 || env.OperationRefs[1] != "01ARZ3NDEKTSV4RRFFQ69G5FAB" {
		t.Fatalf("new operation ref should append once: %#v", env.OperationRefs)
	}

	blank := chatTurnCheckpoint{Goal: "新任务", StreamID: ulid.Make().String()}
	attachContinuation("s2", "test", &blank)
	if blank.Continuation == nil || blank.Continuation.RuntimeEpoch == "" {
		t.Fatal("first save must mint a runtime epoch")
	}
	firstEpoch := blank.Continuation.RuntimeEpoch
	attachContinuation("s2", "newer-writer", &blank)
	if blank.Continuation.RuntimeEpoch != firstEpoch {
		t.Fatalf("epoch must stay pinned: %q vs %q", firstEpoch, blank.Continuation.RuntimeEpoch)
	}

	got, err := combineDurableProviderMessages(
		[]contextapp.Message{{Role: "tool", Content: "[tool-result callId=x]\nok"}},
		[]llmadapter.Message{{Role: llmadapter.RoleUser, Content: "继续"}},
		contextapp.ProviderInfo{ContextWindow: 1000, SafetyCeiling: 1000},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.Role == llmadapter.RoleTool || m.ToolCallID != "" {
			t.Fatalf("S1 fold guard must stay: %#v", got)
		}
	}
}

func TestContinuationExportRedactsPrivateReasoning(t *testing.T) {
	env := &modelfit.ContinuationEnvelope{
		Completeness: modelfit.CompletenessStructuredOnly,
		Protocol:     modelfit.ProtocolCapture{ReasoningContent: "secret chain", Source: modelfit.SourceProvider, Complete: true},
	}
	got := continuationExport(env)
	if got == nil || got.Protocol.ReasoningContent != "" {
		t.Fatalf("export leaked private reasoning: %#v", got)
	}
	if env.Protocol.ReasoningContent != "secret chain" {
		t.Fatal("export must not mutate stored envelope")
	}
}

func TestAttachContinuationClaimsNativeOnlyWhenCodecQualifies(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	attachObtainedProtocol(&cp, "need file")
	pinContinuationIdentity(&cp, continuationIdentity{CodecVersion: modelfit.CodecDeepSeekV1})
	cp.liveProtocol = []llmadapter.Message{
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: []byte(`{}`)},
		}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok"},
	}
	attachContinuation("s1", "test", &cp)
	if cp.Continuation.Completeness != modelfit.CompletenessNativeComplete {
		t.Fatalf("qualified deepseek must be native_complete: %#v", cp.Continuation)
	}
	bare := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	attachObtainedProtocol(&bare, "need file")
	attachContinuation("s1", "test", &bare)
	if bare.Continuation.Completeness == modelfit.CompletenessNativeComplete {
		t.Fatal("no codec must not claim native_complete")
	}
	stale := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	stale.Continuation = &modelfit.ContinuationEnvelope{
		SchemaVersion: "1",
		Completeness:  modelfit.CompletenessNativeComplete,
		CodecVersion:  modelfit.CodecDeepSeekV1,
	}
	attachContinuation("s1", "test", &stale)
	if stale.Continuation.Completeness != modelfit.CompletenessStructuredOnly {
		t.Fatalf("unqualified codec must downgrade native_complete: %#v", stale.Continuation)
	}
}

func TestRememberMessageGroupsKeepsCompletePairsOnly(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	msgs := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "写周报"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: []byte(`{"path":"a.md"}`)},
		}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c2", Name: "workspace.write", Arguments: []byte(`{"path":`)},
		}},
	}
	got := rememberMessageGroups(&cp, msgs)
	if len(got) != 1 || !got[0].Complete || got[0].Tools[0].Content != "ok" {
		t.Fatalf("complete pair lost: %#v", got)
	}
	if cp.Continuation == nil || len(cp.Continuation.MessageGroupRefs) != 1 {
		t.Fatalf("complete ref not stored: %#v", cp.Continuation)
	}
	if !modelfit.CanExecuteToolCalls(got[0].Assistant.ToolCalls) {
		t.Fatal("complete pair must remain executable")
	}
}

func TestResumeKeepsPinnedRuntimeEpoch(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	cp := chatTurnCheckpoint{
		Status:   turnStatusInterrupted,
		Goal:     "写周报",
		StreamID: ulid.Make().String(),
	}
	pinContinuationIdentity(&cp, continuationIdentity{
		ProviderDeploymentRef: "prov-1",
		ModelRequested:        "deepseek-chat",
		RuntimeEpoch:          "epoch-keep",
	})
	if err := e.saveTurnCheckpoint(sessionID, cp); err != nil {
		t.Fatal(err)
	}
	next := chatTurnCheckpoint{Goal: resumeUserPrompt, StreamID: ulid.Make().String()}
	if err := e.reconcileTurnCheckpointOnStart(sessionID, &next); err != nil {
		t.Fatal(err)
	}
	if next.Continuation == nil || next.Continuation.RuntimeEpoch != "epoch-keep" {
		t.Fatalf("resume must keep pinned epoch: %#v", next.Continuation)
	}
	if next.Continuation.ModelRequested != "deepseek-chat" || next.Continuation.ProviderDeploymentRef != "prov-1" {
		t.Fatalf("resume must keep deployment pin: %#v", next.Continuation)
	}
	pinContinuationIdentity(&next, continuationIdentity{ModelRequested: "glm-4", RuntimeEpoch: "epoch-new", ProviderDeploymentRef: "prov-2"})
	if next.Continuation.RuntimeEpoch != "epoch-keep" || next.Continuation.ModelRequested != "deepseek-chat" {
		t.Fatalf("new settings must not rewrite resumed pin: %#v", next.Continuation)
	}
}

func TestNativeReplaySurvivesEngineReload(t *testing.T) {
	e1, _, sessionID, _ := messageEngine(t)
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e1.SetToolRuntime(runtime)

	ctx := context.Background()
	db, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e1.SetMessageGroupStore(db)

	streamID := ulid.Make().String()
	group := modelfit.MessageGroup{
		ID:       ulid.Make().String(),
		Complete: true,
		Assistant: modelfit.ProtocolMessage{Role: "assistant", ToolCalls: []modelfit.ProtocolToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: []byte(`{"path":"a.md"}`)},
		}},
		Tools: []modelfit.ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "ok"}},
	}
	if err := db.PutProtocolMessageGroup(ctx, ownerScope(sessionID), sessionID, streamID, group); err != nil {
		t.Fatal(err)
	}
	cp := chatTurnCheckpoint{
		Status:   turnStatusInterrupted,
		Goal:     "写周报",
		StreamID: streamID,
		Continuation: &modelfit.ContinuationEnvelope{
			Completeness:     modelfit.CompletenessNativeComplete,
			CodecVersion:     modelfit.CodecDeepSeekV1,
			MessageGroupRefs: []string{group.ID},
			Protocol:         modelfit.ProtocolCapture{ReasoningContent: "need file", Source: modelfit.SourceProvider},
		},
	}
	if err := e1.saveTurnCheckpoint(sessionID, cp); err != nil {
		t.Fatal(err)
	}

	e2 := NewEngine(nil, "reload")
	e2.SetToolRuntime(runtime)
	e2.SetMessageGroupStore(db)
	e2.sessions = e1.sessions
	got := e2.nativeReplayMessages(sessionID)
	if len(got) < 2 {
		t.Fatalf("reload must replay native pair: %#v", got)
	}
	var sawTool bool
	for _, m := range got {
		if m.Role == llmadapter.RoleTool {
			sawTool = true
			if m.ToolCallID == "" {
				t.Fatalf("native replay must keep tool_call_id: %#v", got)
			}
		}
	}
	if !sawTool {
		t.Fatalf("native replay missing tool receipt: %#v", got)
	}
	trusted := []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "继续"}}
	env := contextapp.ContextEnvelope{Provider: contextapp.ProviderInfo{Model: "model", ContextWindow: 128000, ReservedOutput: 1024, SystemTokens: 100}}
	assembled, err := assembleExplicitChat(context.Background(), sessionID, env, trusted, got)
	if err != nil {
		t.Fatal(err)
	}
	var assembledTool bool
	for _, m := range assembled {
		if m.Role == llmadapter.RoleTool {
			assembledTool = true
			if m.ToolCallID == "" {
				t.Fatalf("chat.start explicit path must keep tool_call_id: %#v", assembled)
			}
		}
	}
	if !assembledTool {
		t.Fatalf("explicit assembly dropped native pair: %#v", assembled)
	}

	folded, err := combineDurableProviderMessages(
		[]contextapp.Message{{Role: "tool", Content: "[tool-result callId=x]\nok"}},
		[]llmadapter.Message{{Role: llmadapter.RoleUser, Content: "继续"}},
		contextapp.ProviderInfo{ContextWindow: 1000, SafetyCeiling: 1000},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range folded {
		if m.Role == llmadapter.RoleTool || m.ToolCallID != "" {
			t.Fatalf("no-group fold guard must stay: %#v", folded)
		}
	}
}

func TestSaveTurnCheckpointFailsWhenGroupPersistFails(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	e.SetMessageGroupStore(failingGroupStore{})
	cp := chatTurnCheckpoint{
		Goal:     "写周报",
		StreamID: ulid.Make().String(),
		liveProtocol: []llmadapter.Message{
			{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
				{ID: "c1", Name: "workspace.read", Arguments: []byte(`{}`)},
			}},
			{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok"},
		},
	}
	attachObtainedProtocol(&cp, "need file")
	pinContinuationIdentity(&cp, continuationIdentity{CodecVersion: modelfit.CodecDeepSeekV1})
	if err := e.saveTurnCheckpoint(sessionID, cp); err == nil {
		t.Fatal("group persist failure must fail the checkpoint")
	}
}

func TestAttachContinuationKeepsNativeWhenPrivateRefSealed(t *testing.T) {
	cp := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	cp.Continuation = &modelfit.ContinuationEnvelope{
		SchemaVersion:      "1",
		Completeness:       modelfit.CompletenessNativeComplete,
		CodecVersion:       modelfit.CodecDeepSeekV1,
		ProtocolPrivateRef: ulid.Make().String(),
		Protocol:           modelfit.ProtocolCapture{Source: modelfit.SourceProvider},
	}
	attachContinuation("s1", "test", &cp)
	if cp.Continuation.Completeness != modelfit.CompletenessNativeComplete {
		t.Fatalf("sealed private ref must keep native_complete: %#v", cp.Continuation)
	}
}

func TestSaveTurnCheckpointRedactsJournalReasoningAfterSeal(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	ctx := context.Background()
	db, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "seal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e.SetMessageGroupStore(db)
	cp := chatTurnCheckpoint{
		Goal:     "写周报",
		StreamID: ulid.Make().String(),
		liveProtocol: []llmadapter.Message{
			{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
				{ID: "c1", Name: "workspace.read", Arguments: []byte(`{}`)},
			}},
			{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok"},
		},
	}
	attachObtainedProtocol(&cp, "secret chain")
	pinContinuationIdentity(&cp, continuationIdentity{CodecVersion: modelfit.CodecDeepSeekV1})
	if err := e.saveTurnCheckpoint(sessionID, cp); err != nil {
		t.Fatal(err)
	}
	loaded := e.loadTurnCheckpoint(sessionID)
	if loaded.Continuation == nil || loaded.Continuation.Protocol.ReasoningContent != "" {
		t.Fatalf("journal must redact sealed reasoning: %#v", loaded.Continuation)
	}
	if loaded.Continuation.ProtocolPrivateRef == "" || loaded.Continuation.Completeness != modelfit.CompletenessNativeComplete {
		t.Fatalf("sealed native envelope lost: %#v", loaded.Continuation)
	}
	raw, _ := json.Marshal(loaded)
	if strings.Contains(string(raw), "secret chain") {
		t.Fatalf("journal leaked private reasoning: %s", raw)
	}
}

func TestSaveTurnCheckpointAfterModelRefreshesLiveProtocol(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	turn := chatTurnCheckpoint{Goal: "写周报", StreamID: ulid.Make().String()}
	turn.liveProtocol = []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "old header"}}
	live := []llmadapter.Message{
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: []byte(`{}`)},
		}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok"},
	}
	attachObtainedProtocol(&turn, "need file")
	pinContinuationIdentity(&turn, continuationIdentity{CodecVersion: modelfit.CodecDeepSeekV1})
	if err := e.saveTurnCheckpointAfterModel(sessionID, &turn, llmadapter.Response{Reasoning: "need file"}, "", false, live); err != nil {
		t.Fatal(err)
	}
	if len(rememberMessageGroups(&turn, turn.liveProtocol)) != 1 {
		t.Fatalf("after-model save must persist the current tool pair: %#v", turn.liveProtocol)
	}
}

type failingGroupStore struct{}

func (failingGroupStore) PutProtocolMessageGroup(context.Context, string, string, string, modelfit.MessageGroup) error {
	return errors.New("disk full")
}
func (failingGroupStore) ListCompleteProtocolMessageGroups(context.Context, string, string, string) ([]modelfit.MessageGroup, error) {
	return nil, nil
}
func (failingGroupStore) PutProtocolPrivate(context.Context, string, string, []byte, string, string) error {
	return nil
}
func (failingGroupStore) GetProtocolPrivate(context.Context, string, string) ([]byte, string, error) {
	return nil, "", nil
}

func TestValidateToolCallIDsRejectsEmpty(t *testing.T) {
	if err := validateToolCallIDs([]llmadapter.ToolCall{{ID: "c1", Name: "workspace.read"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateToolCallIDs([]llmadapter.ToolCall{{Name: "workspace.read"}}); err == nil || !strings.Contains(err.Error(), "empty tool call id") {
		t.Fatalf("empty id must be refused: %v", err)
	}
}

func TestShouldContinueTurnIgnoresDisableReasoning(t *testing.T) {
	if !shouldContinueTurn("请确认是否继续安装", true, 0, true) {
		t.Fatal("DisableReasoning must not stop a mid-task ask after tools")
	}
	if shouldContinueTurn("文件写好了，下一步打开网页。", true, 0, true) {
		t.Fatal("completed work still must not nudge")
	}
	if shouldContinueTurn("请确认是否继续安装", false, 0, false) {
		t.Fatal("ask without tools must not extra-loop")
	}
}
