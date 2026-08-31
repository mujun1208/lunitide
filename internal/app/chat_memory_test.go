package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/memoryapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func openAppMemory(t *testing.T) (*m8app.MemoryService, *m8app.MemoryOpsService, *m8app.NominationService) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "chat-memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	mem := m8app.NewMemoryService(repo, "local-user")
	mem.SetFTS(store)
	return mem, m8app.NewMemoryOpsService(store), m8app.NewNominationService(repo, mem)
}

func confirmPref(t *testing.T, svc *m8app.MemoryService, content string) {
	t.Helper()
	confirmPrefOn(t, svc, "local-user", content)
}

func confirmPrefOn(t *testing.T, svc *m8app.MemoryService, subject, content string) {
	t.Helper()
	if strings.TrimSpace(subject) == "" {
		subject = "local-user"
	}
	prop, err := svc.ProposeCandidate(context.Background(), m8app.ProposeInput{
		SubjectID: subject,
		Doc: m8core.PayloadDoc{
			Content: content, ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
		},
		Trust: m8core.TrustUntrusted, Actor: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidate(context.Background(), m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "pref-" + prop.Candidate.CandidateID,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkingToSourcesSkipsOtherSessionsAndExpired(t *testing.T) {
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	expired := time.Now().UTC().Add(-time.Hour)
	items := []memory.Memory{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", Layer: memory.LayerWorking, Scope: memory.ScopeProject, Key: "当前任务", Content: "写注入改造"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", Layer: memory.LayerWorking, Scope: memory.ScopeSession, SourceID: &sessionID, Key: "本会话", Content: "只给当前会话"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAC", Layer: memory.LayerWorking, Scope: memory.ScopeSession, SourceID: &other, Key: "别的会话", Content: "不该出现"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAD", Layer: memory.LayerWorking, Scope: memory.ScopeProject, Key: "过期", Content: "旧任务", ExpiresAt: &expired},
	}
	got := workingToSources(items, sessionID)
	joined := ""
	for _, s := range got {
		joined += s.Content
	}
	if !strings.Contains(joined, "写注入改造") || !strings.Contains(joined, "只给当前会话") {
		t.Fatalf("missing current working memories: %q", joined)
	}
	if strings.Contains(joined, "不该出现") || strings.Contains(joined, "旧任务") {
		t.Fatalf("leaked other-session or expired working memory: %q", joined)
	}
}

func TestWorkingToSourcesMarksExpertLastUnconfirmed(t *testing.T) {
	items := []memory.Memory{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", Layer: memory.LayerWorking, Scope: memory.ScopeProject, Key: "任务", Content: "共享任务"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", Layer: memory.LayerWorking, Scope: memory.ScopeProject, Key: "expert:01ARZ3NDEKTSV4RRFFQ69G5FAV:last", Content: "封面改深色"},
	}
	got := workingToSources(items, "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	var last, plain string
	for _, s := range got {
		if strings.Contains(s.Content, "封面") {
			last = s.Content
		}
		if strings.Contains(s.Content, "共享任务") {
			plain = s.Content
		}
	}
	if !strings.Contains(last, "未确认工作摘要") || !strings.Contains(last, "封面改深色") {
		t.Fatalf("expert last must stay working and marked unconfirmed: %q", last)
	}
	if plain == "" || strings.Contains(plain, "未确认工作摘要") {
		t.Fatalf("plain working leaked disclaimer: %q", plain)
	}
}

func TestClipMemorySourcesHonorsLayeredQuotas(t *testing.T) {
	items := []contextapp.ContextSource{{Content: "aaaa"}, {Content: "bbbb"}, {Content: "cccc"}}
	got := clipMemorySources(items, 2, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	got = clipMemorySources(items, 8, 6)
	if len(got) != 1 || got[0].Content != "aaaa" {
		t.Fatalf("byte budget failed: %+v", got)
	}
}

func TestApplyChatMemoryPackFillsEnvelopeSlots(t *testing.T) {
	env := contextapp.ContextEnvelope{}
	applyChatMemoryPack(&env, chatMemoryPack{
		Pinned:    []contextapp.ContextSource{{Type: contextapp.SourcePinnedFacts, Content: "长期事实"}},
		TaskState: []contextapp.ContextSource{{Type: contextapp.SourceTaskState, Content: "当前任务"}},
		Evidence:  []contextapp.ContextSource{{Type: contextapp.SourceRetrievedEvidence, Content: "会话摘要"}},
	})
	if len(env.PinnedFacts) != 1 || env.PinnedFacts[0].Content != "长期事实" {
		t.Fatalf("pinned = %+v", env.PinnedFacts)
	}
	if len(env.TaskState) != 1 || env.TaskState[0].Content != "当前任务" {
		t.Fatalf("task = %+v", env.TaskState)
	}
	if len(env.RelatedEvidence) != 1 || env.RelatedEvidence[0].Content != "会话摘要" {
		t.Fatalf("evidence = %+v", env.RelatedEvidence)
	}
}

func TestMemorySubjectIDFallsBackWithoutIdentity(t *testing.T) {
	e := NewEngine(nil, "test")
	if e.memorySubjectID() != memoryOpsLegacySubject {
		t.Fatalf("memory subject = %q", e.memorySubjectID())
	}
}

func TestChatMemorySettingsUsesIdentitySubject(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "mem-ident.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	ops := m8app.NewMemoryOpsService(store)
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: ident.SubjectID(), MemoryEnabled: false, AutoNominate: false, GrowthDays: 21,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetIdentityPeopleServices(ident, nil)
	e.SetMemoryOpsService(ops)
	st := e.chatMemorySettings(ctx)
	if st.SubjectID != ident.SubjectID() || st.MemoryEnabled || st.GrowthDays != 21 {
		t.Fatalf("settings = %+v want subject %s", st, ident.SubjectID())
	}
}

func TestFormatChatMemorySummaryCountsSlots(t *testing.T) {
	if formatChatMemorySummary(chatMemoryPack{}) != "" {
		t.Fatal("empty pack must stay silent")
	}
	got := formatChatMemorySummary(chatMemoryPack{
		Prefs:     []string{"a", "b"},
		Pinned:    []contextapp.ContextSource{{Content: "p1"}, {Content: "p2"}, {Content: "p3"}},
		TaskState: []contextapp.ContextSource{{Content: "t"}},
		Evidence:  []contextapp.ContextSource{{Content: "e1"}, {Content: "e2"}, {Content: "e3"}, {Content: "e4"}},
	})
	if got != "注入记忆：偏好「a」「b」 · 置顶 3 · 任务 1 · 证据 4" {
		t.Fatalf("summary = %q", got)
	}
}

func TestPeopleCompanionMemoryHintInjectsPrefs(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	confirmPref(t, mem, "回答默认使用中文")
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	got := e.peopleCompanionMemoryHint(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAW", "写注释")
	if !strings.Contains(got, "回答默认使用中文") {
		t.Fatalf("hint = %q", got)
	}
}

func TestLocalBrainHintAndCompanionShareMemorySubject(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	confirmPref(t, mem, "回答默认使用中文")
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	companion := e.peopleLocalBrainMemoryHint(context.Background(), sessionID, "写注释")
	peopleHint := e.peopleCompanionMemoryHint(context.Background(), sessionID, "继续刚才的")
	if !strings.Contains(companion, "回答默认使用中文") {
		t.Fatalf("local-brain hint = %q", companion)
	}
	if !strings.Contains(peopleHint, "回答默认使用中文") {
		t.Fatalf("people hint = %q", peopleHint)
	}
	if e.chatMemorySettings(context.Background()).SubjectID != e.ensureChatMemorySubject(context.Background()) {
		t.Fatal("companion and colleague memory must share the identity subject")
	}
}

func TestPrepareChatMemoryKeepsPrefsWhenRecallDisabled(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	confirmPref(t, mem, "回答默认使用中文")
	if err := ops.SettingsUpdate(context.Background(), m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: false, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{Query: "写一段代码注释"})
	if pack.Enabled {
		t.Fatal("memoryEnabled=false must disable query inject")
	}
	if len(pack.Prefs) != 1 || pack.Prefs[0] != "回答默认使用中文" {
		t.Fatalf("prefs = %v, confirmed preferences must still inject", pack.Prefs)
	}
	if len(pack.Pinned) != 0 || len(pack.Evidence) != 0 {
		t.Fatalf("disabled inject leaked slots: %+v", pack)
	}
}

func TestPreferExpertOwnedMemories(t *testing.T) {
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	items := []memory.Memory{
		{Key: "任务", Content: "共享"},
		{Key: "expert:" + expertID + ":语气", Content: "专家私有"},
	}
	got := preferExpertOwnedMemories(items, []string{expertID})
	if got[0].Content != "专家私有" || got[1].Content != "共享" {
		t.Fatalf("order = %#v", got)
	}
}

func TestIsolateExpertMemoriesDropsForeignBuckets(t *testing.T) {
	mine := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	items := []memory.Memory{
		{Key: "任务", Content: "共享"},
		{Key: sessionLastMemoryKey, Content: "刚才的封面"},
		{Key: "expert:" + mine + ":语气", Content: "我的"},
		{Key: "expert:" + other + ":语气", Content: "别人的"},
	}
	got := isolateExpertMemories(items, []string{mine})
	if len(got) != 3 || got[0].Content != "我的" {
		t.Fatalf("isolated = %#v", got)
	}
	plain := isolateExpertMemories(items, nil)
	if len(plain) != 2 {
		t.Fatalf("no-expert turn leaked buckets: %#v", plain)
	}
	shared := plain[0].Content + " " + plain[1].Content
	if !strings.Contains(shared, "共享") || !strings.Contains(shared, "刚才的封面") {
		t.Fatalf("session last must survive a no-expert turn: %#v", plain)
	}
}

func TestMaybeWriteExpertTurnMemories(t *testing.T) {
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	e.sessionExperts = stubSessionExperts{ids: []string{expertID}}
	user := "请按上次语气继续写封面要点和结构"
	asst := strings.Repeat("这页讲结论，下一页讲证据。", 8)
	e.maybeWriteExpertTurnMemories(context.Background(), sessionID, user, asst)
	if len(store.items) != 1 {
		t.Fatalf("want 1 memory, got %#v", store.items)
	}
	if store.items[0].Key != "expert:"+expertID+":last" {
		t.Fatalf("key = %q", store.items[0].Key)
	}
	if !strings.Contains(store.items[0].Content, "封面") {
		t.Fatalf("content = %q", store.items[0].Content)
	}
	e.maybeWriteExpertTurnMemories(context.Background(), sessionID, user, asst+" 更新")
	if len(store.items) != 1 {
		t.Fatal("upsert must not duplicate")
	}
	if !strings.Contains(store.items[0].Content, "更新") {
		t.Fatalf("update missed: %q", store.items[0].Content)
	}
}

func TestWriteExpertLastMemoryFromPeoplePath(t *testing.T) {
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	user := "请按上次语气继续写封面要点和结构"
	asst := strings.Repeat("这页讲结论，下一页讲证据。", 8)
	e.writeExpertLastMemory(context.Background(), sessionID, expertID, user, asst)
	if len(store.items) != 1 || store.items[0].Key != "expert:"+expertID+":last" {
		t.Fatalf("people write = %#v", store.items)
	}
	got := e.peopleCompanionMemoryHint(context.Background(), sessionID, "继续封面", expertID)
	if !strings.Contains(got, "工作记忆") || !strings.Contains(got, "封面") || !strings.Contains(got, "未确认工作摘要") {
		t.Fatalf("people hint must inject working last as unconfirmed: %q", got)
	}
}

func TestWriteSessionLastMemoryCrossesCompanionAndPeople(t *testing.T) {
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = store
	user := "以后回答请默认用中文，并且封面用深色"
	asst := strings.Repeat("好，封面改深色，后文都用中文写。", 6)
	e.writeSessionLastMemory(context.Background(), sessionID, user, asst)
	if len(store.items) != 1 || store.items[0].Key != sessionLastMemoryKey {
		t.Fatalf("session last = %#v", store.items)
	}
	e.writeSessionLastMemory(context.Background(), sessionID, user, asst+" 已记下")
	if len(store.items) != 1 {
		t.Fatal("upsert must not duplicate session last")
	}
	companion := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "继续刚才的", SessionID: sessionID, Companion: true,
	})
	if len(companion.TaskState) != 1 || !strings.Contains(companion.TaskState[0].Content, "未确认工作摘要") || !strings.Contains(companion.TaskState[0].Content, "深色") {
		t.Fatalf("companion must inject session last: %+v", companion.TaskState)
	}
	peopleSession := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	people := e.peopleCompanionMemoryHint(context.Background(), peopleSession, "继续刚才的")
	if !strings.Contains(people, "工作记忆") || !strings.Contains(people, "深色") || !strings.Contains(people, "未确认工作摘要") {
		t.Fatalf("people continue-just-now missed session last: %q", people)
	}
	e.writeSessionLastMemory(context.Background(), sessionID, "短", "也短")
	if !strings.Contains(store.items[0].Content, "已记下") {
		t.Fatalf("short turn must not overwrite: %q", store.items[0].Content)
	}
	ackStore := &layerMemoryStub{}
	e.memories = ackStore
	e.writeSessionLastMemory(context.Background(), sessionID, user, "好，记下了。")
	if len(ackStore.items) != 1 || !strings.Contains(ackStore.items[0].Content, "深色") {
		t.Fatalf("preference ack must write session last: %#v", ackStore.items)
	}
}

type sessionLastCompleteAdapter struct{}

func (sessionLastCompleteAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, nil
}
func (sessionLastCompleteAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}
func (sessionLastCompleteAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	text := "好，以后回答默认用中文，封面改深色。"
	if err := emit(gateway.Delta{Text: text}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Message: gateway.Message{Content: text}}, nil
}

func TestChatStartWritesSessionLastBeforeCompleted(t *testing.T) {
	store := &layerMemoryStub{}
	spy := &appendAssistantSpy{}
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.sessions = sessionGetStub{projectID: chatAttachmentProjectID}
	e.memories = store
	e.messages = spy
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return sessionLastCompleteAdapter{}, nil
	})
	events := make(chan bridge.Event, 16)
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"以后回答请默认用中文，并且封面用深色"}]}`
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(event bridge.Event) error {
		events <- event
		return nil
	})
	if !resp.OK {
		t.Fatalf("chat.start: %#v", resp)
	}
	terminal := terminalEvent(t, events)
	if terminal.Type != bridge.EventCompleted {
		t.Fatalf("terminal=%s", terminal.Type)
	}
	if len(store.items) != 1 || store.items[0].Key != sessionLastMemoryKey {
		t.Fatalf("session last after chat.start = %#v", store.items)
	}
	if !strings.Contains(store.items[0].Content, "封面用深色") {
		t.Fatalf("session last missed user goal: %q", store.items[0].Content)
	}
	companion := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "继续刚才的", SessionID: chatAttachmentOtherID, Companion: true,
	})
	if len(companion.TaskState) != 1 || !strings.Contains(companion.TaskState[0].Content, "未确认工作摘要") || !strings.Contains(companion.TaskState[0].Content, "深色") {
		t.Fatalf("companion continue must see session last written by chat.start: %+v", companion.TaskState)
	}
}

func TestWriteExpertLastMemoryNominatesForConfirm(t *testing.T) {
	mem, ops, nom := openAppMemory(t)
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetM10NominationService(nom)
	e.SetMemoryOpsService(ops)
	e.sessions = sessionGetStub{projectID: "01ARZ3NDEKTSV4RRFFQ69G5FAY"}
	e.memories = store
	ctx := context.Background()
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: true, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	user := "请按上次语气继续写封面要点和结构"
	asst := strings.Repeat("这页讲结论，下一页讲证据。", 8)
	e.writeExpertLastMemory(ctx, sessionID, expertID, user, asst)
	if len(store.items) != 1 || store.items[0].Key != "expert:"+expertID+":last" {
		t.Fatalf("working last = %#v", store.items)
	}
	items, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil || len(items) != 1 || items[0].Reason != expertLastNominationReason {
		t.Fatalf("expert nomination = %+v err=%v", items, err)
	}
	e.writeExpertLastMemory(ctx, sessionID, expertID, user, asst)
	again, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil || len(again) != 1 {
		t.Fatalf("duplicate nomination = %+v err=%v", again, err)
	}
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: true, AutoNominate: true, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.maybeAutoNominateTurn(ctx, sessionID, user, asst, "01ARZ3NDEKTSV4RRFFQ69G5FAV", false); err != nil {
		t.Fatal(err)
	}
	deduped, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil || len(deduped) != 1 {
		t.Fatalf("auto-nominate must not duplicate expert gist: %+v err=%v", deduped, err)
	}
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: false, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	e.writeExpertLastMemory(ctx, sessionID, expertID, "另一条足够长的用户请求请记住要点", strings.Repeat("另一段足够长的专家回答内容。", 8))
	disabled, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil || len(disabled) != 1 {
		t.Fatalf("disabled memory still nominated: %+v err=%v", disabled, err)
	}
}

func TestPrepareChatMemoryNilServicesDoNotBlock(t *testing.T) {
	e := NewEngine(nil, "test")
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{Query: "hello there friend"})
	if !pack.Enabled {
		t.Fatal("missing ops defaults to memory enabled")
	}
	if len(pack.Prefs) != 0 || len(pack.Pinned) != 0 {
		t.Fatalf("empty services leaked inject: %+v", pack)
	}
}

func TestPrepareChatMemoryMapsDomainLayers(t *testing.T) {
	e := NewEngine(nil, "test")
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = &layerMemoryStub{items: []memory.Memory{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ProjectID: projectID, Layer: memory.LayerWorking, Scope: memory.ScopeProject, Key: "任务", Content: "改造记忆注入"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", ProjectID: projectID, Layer: memory.LayerProcedural, Scope: memory.ScopeProject, Key: "规范", Content: "代码注释用中文"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAC", ProjectID: projectID, Layer: memory.LayerEpisodic, Scope: memory.ScopeSession, Key: "上次", Content: "讨论过中文注释风格"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAD", ProjectID: projectID, Layer: memory.LayerSemantic, Scope: memory.ScopeProject, Key: "架构", Content: "中文文档优先"},
	}}
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "中文", SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
	})
	if len(pack.TaskState) != 1 || !strings.Contains(pack.TaskState[0].Content, "改造记忆注入") {
		t.Fatalf("working layer missing: %+v", pack.TaskState)
	}
	if len(pack.Pinned) != 1 || !strings.Contains(pack.Pinned[0].Content, "代码注释用中文") {
		t.Fatalf("procedural not pinned: %+v", pack.Pinned)
	}
	if len(pack.Evidence) != 2 {
		t.Fatalf("episodic+semantic evidence = %+v", pack.Evidence)
	}
}

func TestCompanionSkipsEvidenceChannel(t *testing.T) {
	e := NewEngine(nil, "test")
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = &layerMemoryStub{items: []memory.Memory{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAC", ProjectID: projectID, Layer: memory.LayerEpisodic, Scope: memory.ScopeSession, Key: "上次", Content: "讨论过中文注释风格"},
	}}
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "中文", SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Companion: true,
	})
	if len(pack.Evidence) != 0 {
		t.Fatalf("companion must skip evidence: %+v", pack.Evidence)
	}
}

func TestPeopleMemoryHintUsesFullSessionPack(t *testing.T) {
	e := NewEngine(nil, "test")
	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	expert := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	e.sessions = sessionGetStub{projectID: projectID}
	e.memories = &layerMemoryStub{items: []memory.Memory{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAC", ProjectID: projectID, Layer: memory.LayerEpisodic, Scope: memory.ScopeSession, Key: "上次", Content: "讨论过中文注释风格"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAD", ProjectID: projectID, Layer: memory.LayerEpisodic, Scope: memory.ScopeSession, Key: "expert:" + other + ":上次", Content: "别人专家的中文证据"},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAE", ProjectID: projectID, Layer: memory.LayerProcedural, Scope: memory.ScopeProject, Key: "expert:" + expert + ":语气", Content: "中文封面用深色"},
	}}
	got := e.peopleCompanionMemoryHint(context.Background(), sessionID, "中文", expert)
	if !strings.Contains(got, "讨论过中文注释风格") {
		t.Fatalf("people must inject evidence, not companion-tight pack: %q", got)
	}
	if !strings.Contains(got, "中文封面用深色") {
		t.Fatalf("expert owned pinned missing: %q", got)
	}
	if strings.Contains(got, "别人专家") {
		t.Fatalf("foreign expert leaked: %q", got)
	}
}

func TestMaybeAutoNominateRespectsSwitchAndConfirmationInbox(t *testing.T) {
	mem, ops, nom := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetM10NominationService(nom)
	e.SetMemoryOpsService(ops)
	ctx := context.Background()
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: true, AutoNominate: true, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	st, err := ops.SettingsGet(ctx, memoryOpsLegacySubject)
	if err != nil || !st.AutoNominate || !st.MemoryEnabled {
		t.Fatalf("settings after update = %+v err=%v", st, err)
	}
	if e.memoryOps == nil || e.m10nomination == nil || e.m8memory == nil {
		t.Fatal("engine memory services not wired")
	}
	if eng := e.chatMemorySettings(ctx); !eng.AutoNominate || !eng.MemoryEnabled {
		t.Fatalf("engine settings %+v", eng)
	}
	if err := e.maybeAutoNominateTurn(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"帮我把注释规范写进项目",
		"好的，后续代码注释一律使用中文，并且保持现有确认台流程不变。这是一条足够长的要点，方便自动提名为待确认候选。",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV", false); err != nil {
		t.Fatalf("auto nominate: %v", err)
	}
	pendingCands, _ := mem.ListPendingCandidates(ctx, 20)
	items, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil || len(items) != 1 || items[0].Reason != "本轮对话自动提名" {
		t.Fatalf("auto nominate inbox = %+v pending=%+v err=%v", items, pendingCands, err)
	}
	if _, err := mem.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: items[0].CandidateID, Token: items[0].ConfirmationToken, Action: "confirm", RequestID: "auto-1",
	}); err != nil {
		t.Fatal(err)
	}

	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: true, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	e.maybeAutoNominateTurn(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"再写一条不应该出现的提名",
		"这段足够长的回答也不应该在关闭自动提名时进入确认台。",
		"01ARZ3NDEKTSV4RRFFQ69G5FAX", false)
	pending, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pending {
		if strings.Contains(item.Content, "不应该出现") {
			t.Fatal("autoNominate=false still nominated")
		}
	}

	if err := e.maybeAutoNominateTurn(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"以后回答请默认用中文，并且封面用深色",
		"好，记下了。",
		"01ARZ3NDEKTSV4RRFFQ69G5FAD", false); err != nil {
		t.Fatalf("preference nominate: %v", err)
	}
	prefItems, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range prefItems {
		if item.Reason == preferenceNominationReason && strings.Contains(item.Content, "默认用中文") {
			found = true
		}
	}
	if !found {
		t.Fatalf("explicit preference must reach inbox when autoNominate is off: %+v", prefItems)
	}
	if !looksLikePreferenceTurn("以后回答请默认用中文") || looksLikePreferenceTurn("下次开会几点") {
		t.Fatal("preference detector")
	}
}

func TestRecordPeopleTurnMemoryNominatesPreference(t *testing.T) {
	mem, ops, nom := openAppMemory(t)
	store := &layerMemoryStub{}
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetM10NominationService(nom)
	e.SetMemoryOpsService(ops)
	e.sessions = sessionGetStub{projectID: "01ARZ3NDEKTSV4RRFFQ69G5FAY"}
	e.memories = store
	ctx := context.Background()
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: true, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	user := "以后回答请默认用中文，并且封面用深色"
	asst := "好，记下了。"
	e.recordPeopleTurnMemory(ctx, sessionID, expertID, user, asst, "01ARZ3NDEKTSV4RRFFQ69G5FAD")
	if len(store.items) != 1 || store.items[0].Key != sessionLastMemoryKey {
		t.Fatalf("people must write session last even when expert last is short: %#v", store.items)
	}
	items, err := nom.ListNominations(ctx, "nominated", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Reason == preferenceNominationReason && strings.Contains(item.Content, "默认用中文") {
			found = true
		}
	}
	if !found {
		t.Fatalf("colleague preference must reach inbox: %+v", items)
	}
}

func TestChatStartInjectsConfirmedPrefsNotPending(t *testing.T) {
	mem, _, _ := openAppMemory(t)
	prop, err := mem.ProposeCandidate(context.Background(), m8app.ProposeInput{
		SubjectID: "local-user",
		Doc: m8core.PayloadDoc{
			Content: "pending-secret-must-not-appear", ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
		},
		Inferred: true, Trust: m8core.TrustUntrusted, Actor: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = prop
	confirmPref(t, mem, "回答默认使用中文")

	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.SetM8MemoryServices(mem)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","messages":[{"role":"user","content":"这段代码怎么写注释"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	sys := capturedSkillChatSystem(t, requests)
	if !strings.Contains(sys, "回答默认使用中文") {
		t.Fatalf("confirmed preference missing from system: %q", sys)
	}
	if !strings.Contains(sys, "[持久记忆]") {
		t.Fatalf("Hermes-style persistent memory header missing: %q", sys)
	}
	if strings.Contains(sys, "pending-secret-must-not-appear") {
		t.Fatalf("unconfirmed candidate leaked into system: %q", sys)
	}
}

type sessionGetStub struct{ projectID string }

func (sessionGetStub) Create(context.Context, string, string, any, session.Session) (session.Session, error) {
	return session.Session{}, nil
}
func (sessionGetStub) Update(context.Context, string, string, any, string, int64, string, bool) (session.Session, error) {
	return session.Session{}, nil
}
func (sessionGetStub) List(context.Context, session.Filter) ([]session.Session, error) {
	return nil, nil
}
func (sessionGetStub) Delete(context.Context, string) error { return nil }
func (s sessionGetStub) Get(_ context.Context, id string) (session.Session, error) {
	now := time.Now().UTC()
	return session.Session{ID: id, ProjectID: s.projectID, Title: "Chat", Status: session.StatusActive, CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}

type layerMemoryStub struct{ items []memory.Memory }

func (s *layerMemoryStub) Get(context.Context, string) (*memory.Memory, error) {
	return nil, memoryapp.ErrMemoryNotFound
}
func (s *layerMemoryStub) ListByProject(_ context.Context, _ string, layer memory.Layer) ([]memory.Memory, error) {
	var out []memory.Memory
	for _, item := range s.items {
		if layer == "" || item.Layer == layer {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *layerMemoryStub) Search(_ context.Context, _ string, query string) ([]memory.Memory, error) {
	q := strings.ToLower(query)
	var out []memory.Memory
	for _, item := range s.items {
		if strings.Contains(strings.ToLower(item.Key+" "+item.Content), q) {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *layerMemoryStub) Create(_ context.Context, m memory.Memory) (memory.Memory, error) {
	if m.ID == "" {
		m.ID = "01ARZ3NDEKTSV4RRFFQ69G5FZZ"
	}
	s.items = append(s.items, m)
	return m, nil
}
func (s *layerMemoryStub) UpdateContent(_ context.Context, id, content string) error {
	for i, item := range s.items {
		if item.ID == id {
			s.items[i].Content = content
			return nil
		}
	}
	return memoryapp.ErrMemoryNotFound
}
func (s *layerMemoryStub) Delete(context.Context, string) error { return nil }

func TestMemorySearchAndGetUseConfirmedFactsOnly(t *testing.T) {
	mem, _, _ := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	confirmPref(t, mem, "用户喜欢中文注释")
	ctx := context.Background()
	search, err := e.invokeMemorySearch(ctx, []byte(`{"query":"中文注释"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.Output, "中文注释") {
		t.Fatalf("search = %q", search.Output)
	}
	if strings.Contains(search.Output, "用户刚才说") || strings.Contains(search.Output, "raw transcript") {
		t.Fatalf("search leaked transcript: %q", search.Output)
	}
	id, _, ok := strings.Cut(search.Output, "\t")
	if !ok || id == "" {
		t.Fatalf("search missing id: %q", search.Output)
	}
	got, err := e.invokeMemoryGet(ctx, []byte(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Output, "中文注释") {
		t.Fatalf("get = %q", got.Output)
	}
	missing, err := e.invokeMemoryGet(ctx, []byte(`{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
	if err != nil || missing.Output != "confirmed memory not found" {
		t.Fatalf("missing get = %q err=%v", missing.Output, err)
	}
}

func TestMemoryGetIsolatesIdentitySubject(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "mem-get.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	mem := m8app.NewMemoryService(store.AgentRuntimeRepository(), memoryOpsLegacySubject)
	e := NewEngine(nil, "test")
	e.SetIdentityPeopleServices(ident, nil)
	e.SetM8MemoryServices(mem)
	mine := ident.SubjectID()
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	prop, err := mem.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: other,
		Doc: m8core.PayloadDoc{
			Content: "别人的确认记忆", ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
		},
		Trust: m8core.TrustUntrusted, Actor: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "get-other",
	}); err != nil {
		t.Fatal(err)
	}
	confirmPrefOn(t, mem, mine, "我的确认记忆")
	hidden, err := e.invokeMemoryGet(ctx, []byte(`{"id":"`+prop.Candidate.CandidateID+`"}`))
	if err != nil || hidden.Output != "confirmed memory not found" {
		t.Fatalf("foreign get = %q err=%v", hidden.Output, err)
	}
}
