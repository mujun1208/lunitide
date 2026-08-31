package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	c4ProviderID     = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	c4ModelID        = "glm-cross"
	c4PeerID         = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	c4ForeignSubject = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	c4AgentReply     = "好，幻灯片继续用深色。"
	c4OwnPref        = "晚上用深色主题"
	c4SlidePref      = "继续刚才的幻灯片用深色"
	c4ForeignPref    = "别人的偏好不该注入"
	c4ForeignNote    = "继续刚才的别人笔记不该注入"
)

type c4TurnProvider struct{ providerRepositoryStub }

func (c4TurnProvider) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{
		ID: c4ProviderID, Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com", CredentialRef: "credential-ref",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{
			ModelID: c4ModelID, DisplayName: "C4", IsDefault: true,
			Kind: provider.KindLLM, KindDefault: true, ContextWindow: 128000,
		}},
	}, nil
}

func (p c4TurnProvider) List(ctx context.Context, _ provider.Filter) ([]provider.Provider, error) {
	item, err := p.Get(ctx, c4ProviderID)
	if err != nil {
		return nil, err
	}
	return []provider.Provider{item}, nil
}

type c4CaptureAdapter struct {
	ch    chan gateway.Request
	reply string
}

func (a c4CaptureAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	select {
	case a.ch <- req:
	default:
	}
	return gateway.Response{Message: gateway.Message{Content: a.reply}}, nil
}

func (a c4CaptureAdapter) Stream(_ context.Context, _ []byte, req gateway.Request, _ func(gateway.Delta) error) (gateway.Response, error) {
	select {
	case a.ch <- req:
	default:
	}
	return gateway.Response{}, errors.New("stop after capture")
}

func (c4CaptureAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}

// TestCrossSurfaceTurnContract is C4: 文字写偏好 → 月伴续 → 同事 @专家「继续刚才的」.
// One store, one identity subject. Handlers: chat.start, chat.start companion,
// people.thread.open / send → SendAs. Colleague BoundSession ≠ thread.
func TestCrossSurfaceTurnContract(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "c4.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	subject := ident.SubjectID()
	if subject == "" || subject == memoryOpsLegacySubject {
		t.Fatalf("identity subject = %q", subject)
	}

	mem := m8app.NewMemoryService(store.AgentRuntimeRepository(), memoryOpsLegacySubject)
	ops := m8app.NewMemoryOpsService(store)
	confirmPrefOn(t, mem, subject, c4OwnPref)
	confirmPrefOn(t, mem, subject, c4SlidePref)
	confirmPrefOn(t, mem, c4ForeignSubject, c4ForeignPref)
	confirmPrefOn(t, mem, c4ForeignSubject, c4ForeignNote)

	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: c4PeerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}

	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tools.Close() })

	captured := make(chan gateway.Request, 8)
	e := NewEngineWithSessions(c4TurnProvider{}, projectapp.New(store, store), sessionapp.New(store, store), "test", streamTestLease{})
	e.SetIdentityPeopleServices(ident, roster)
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	e.SetToolRuntime(tools)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return c4CaptureAdapter{ch: captured, reply: c4AgentReply}, nil
	})

	if e.memorySubjectID() != subject {
		t.Fatalf("memory subject = %q want %q", e.memorySubjectID(), subject)
	}
	st := e.chatMemorySettings(ctx)
	if st.SubjectID != subject {
		t.Fatalf("settings subject = %q want %q", st.SubjectID, subject)
	}

	prefer := handleChatPrefer(e, ctx, validRequest("chat.prefer", `{"providerId":"`+c4ProviderID+`","modelId":"`+c4ModelID+`"}`))
	if !prefer.OK {
		t.Fatalf("prefer: %#v", prefer)
	}

	textReq := c4StartChat(t, e, captured, `{"providerId":"`+c4ProviderID+`","modelId":"`+c4ModelID+`","messages":[{"role":"user","content":"记住晚上用深色主题"}]}`)
	c4AssertOwnMemory(t, "text chat.start", c4SystemContent(textReq))
	if !strings.Contains(c4SystemContent(textReq), "[持久记忆]") {
		t.Fatalf("text system missing preference block: %q", c4SystemContent(textReq))
	}

	companionReq := c4StartChat(t, e, captured, `{"providerId":"`+c4ProviderID+`","modelId":"`+c4ModelID+`","companion":true,"messages":[{"role":"user","content":"继续刚才的"}]}`)
	companionSys := c4SystemContent(companionReq)
	c4AssertOwnMemory(t, "companion chat.start", companionSys)
	if !strings.Contains(companionSys, "第一句") {
		t.Fatalf("companion system missing voice instruction: %q", companionSys)
	}
	if strings.Contains(companionSys, "相关证据") {
		t.Fatalf("companion leaked evidence slot: %q", companionSys)
	}

	opened := peopleOK[struct {
		Thread   map[string]any   `json:"thread"`
		Messages []map[string]any `json:"messages"`
	}](t, e, "people.thread.open", map[string]any{"peerSubjectId": c4PeerID})
	threadID, _ := opened.Thread["threadId"].(string)
	if threadID == "" {
		t.Fatalf("people.thread.open = %#v", opened.Thread)
	}

	turnIdent, err := e.conversationIdentityForPeople(ctx, threadID, "PPT专家", []string{c4PeerID})
	if err != nil {
		t.Fatal(err)
	}
	bound := turnIdent.BoundSessionID
	if bound == "" || bound == threadID || turnIdent.Kind != identityKindPeople {
		t.Fatalf("identity = %#v thread=%q", turnIdent, threadID)
	}
	if turnIdent.MemorySubjectID != subject {
		t.Fatalf("people memory subject = %q want %q", turnIdent.MemorySubjectID, subject)
	}
	sessionIdent := e.conversationIdentityForSession(ctx, bound, false)
	if sessionIdent.Kind != identityKindSession || sessionIdent.BoundSessionID != bound || sessionIdent.MemorySubjectID != subject {
		t.Fatalf("session identity = %#v", sessionIdent)
	}
	if sessionIdent.sessionKey("") != bound {
		t.Fatalf("session key = %q", sessionIdent.sessionKey(""))
	}
	again, err := e.ensurePeopleBoundSession(ctx, threadID, "PPT专家")
	if err != nil || again != bound {
		t.Fatalf("bound session drifted: %q %v", again, err)
	}

	continueText := "继续刚才的 @PPT专家 "
	peopleIntent := turnIntentForPeople(continueText)
	if peopleIntent.Surface != SurfacePeople || !hasMention(peopleIntent.Mentions, "member", "PPT专家") {
		t.Fatalf("people intent = %#v", peopleIntent)
	}
	sessionIntent := turnIntentForChat(false, "继续刚才的 [引用专家 PPT专家|"+c4PeerID+"]", "", "full-access", nil)
	if sessionIntent.Surface != SurfaceSession || !hasMention(sessionIntent.Mentions, "expert", "PPT专家") {
		t.Fatalf("session intent = %#v", sessionIntent)
	}
	c4AssertSharedMentionVectors(t)

	sent := peopleOK[struct {
		Message map[string]any `json:"message"`
	}](t, e, "people.thread.send", map[string]any{"threadId": threadID, "kind": "text", "body": continueText})
	if sent.Message["body"] != continueText {
		t.Fatalf("people.thread.send = %#v", sent.Message)
	}

	peopleReq := c4WaitCaptured(t, captured, "你是独立智能体", 4*time.Second)
	peopleSys := c4SystemContent(peopleReq)
	c4AssertOwnMemory(t, "people.thread.send", peopleSys)
	if !strings.Contains(peopleSys, "PPT专家") {
		t.Fatalf("people system missing agent name: %q", peopleSys)
	}
	c4WaitPeopleSendAs(t, roster, threadID, c4PeerID, c4AgentReply)

	foreignGet, err := e.invokeMemoryGet(ctx, []byte(`{"id":"`+c4ForeignSubject+`"}`))
	if err != nil || foreignGet.Output != "confirmed memory not found" {
		t.Fatalf("memory.get leaked: %q err=%v", foreignGet.Output, err)
	}

	e.saveTurnCheckpoint(bound, chatTurnCheckpoint{Status: turnStatusInterrupted, Goal: "继续刚才的幻灯片"})
	inspect := handleChatTurnGet(e, ctx, validRequest("chat.turn.get", `{"sessionId":"`+bound+`"}`))
	if !inspect.OK {
		t.Fatalf("inspect: %#v", inspect)
	}
	payload, _ := inspect.Payload.(map[string]any)
	if payload["status"] != turnStatusInterrupted {
		t.Fatalf("inspect payload = %#v", inspect.Payload)
	}
}

func c4StartChat(t *testing.T, e *Engine, captured <-chan gateway.Request, payload string) gateway.Request {
	t.Helper()
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !resp.OK {
		t.Fatalf("chat.start failed: %#v payload=%s", resp, payload)
	}
	return c4WaitCaptured(t, captured, "[持久记忆]", 2*time.Second)
}

func c4WaitCaptured(t *testing.T, captured <-chan gateway.Request, needle string, timeout time.Duration) gateway.Request {
	t.Helper()
	deadline := time.After(timeout)
	var last string
	for {
		select {
		case req := <-captured:
			sys := c4SystemContent(req)
			last = sys
			if needle == "" || strings.Contains(sys, needle) {
				return req
			}
		case <-deadline:
			t.Fatalf("timed out waiting for captured request containing %q last=%q", needle, last)
		}
	}
}

func c4WaitPeopleSendAs(t *testing.T, roster *people.Service, threadID, peerID, wantBody string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var last []people.Message
	for time.Now().Before(deadline) {
		msgs, err := roster.ListMessages(context.Background(), threadID, 40)
		if err != nil {
			t.Fatal(err)
		}
		last = msgs
		for _, msg := range msgs {
			if msg.SenderID == peerID && strings.Contains(msg.Body, wantBody) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("people SendAs %q from %s not observed: %#v", wantBody, peerID, last)
}

func c4SystemContent(req gateway.Request) string {
	var b strings.Builder
	for _, msg := range req.Messages {
		if msg.Role == gateway.RoleSystem {
			b.WriteString(msg.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func c4AssertOwnMemory(t *testing.T, label, system string) {
	t.Helper()
	if !strings.Contains(system, c4OwnPref) && !strings.Contains(system, c4SlidePref) {
		t.Fatalf("%s missing own pref: %q", label, system)
	}
	if strings.Contains(system, c4ForeignPref) || strings.Contains(system, "别人笔记") || strings.Contains(system, c4ForeignNote) {
		t.Fatalf("%s leaked foreign subject: %q", label, system)
	}
}

func c4AssertSharedMentionVectors(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "session", "turnMentions.vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name string        `json:"name"`
		Text string        `json:"text"`
		Want []TurnMention `json:"want"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("mention vectors empty")
	}
	for _, vec := range vectors {
		got := ParseTurnMentions(vec.Text)
		if len(got) != len(vec.Want) {
			t.Fatalf("vector %s len=%d want %d got=%#v", vec.Name, len(got), len(vec.Want), got)
		}
		for i, want := range vec.Want {
			if got[i].Kind != want.Kind || got[i].ID != want.ID || got[i].Name != want.Name {
				t.Fatalf("vector %s [%d] = %#v want %#v", vec.Name, i, got[i], want)
			}
		}
	}
}

func hasMention(items []TurnMention, kind, name string) bool {
	for _, item := range items {
		if item.Kind == kind && item.Name == name {
			return true
		}
	}
	return false
}
