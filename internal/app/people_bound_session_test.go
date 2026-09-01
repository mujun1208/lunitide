package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestPeopleBoundSessionNotThreadID(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "bound.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, projectapp.New(store, store), sessionapp.New(store, store), "test", nil)
	e.SetIdentityPeopleServices(ident, roster)

	if _, err := e.ensurePeopleBoundSession(ctx, "short", "PPT专家"); err == nil {
		t.Fatal("non-ULID thread must not bind")
	}
	sessionID, err := e.ensurePeopleBoundSession(ctx, thread.ThreadID, "PPT专家")
	if err != nil {
		t.Fatal(err)
	}
	if sessionID == "" || sessionID == thread.ThreadID {
		t.Fatalf("bound session %q must not equal thread %q", sessionID, thread.ThreadID)
	}
	again, err := e.ensurePeopleBoundSession(ctx, thread.ThreadID, "PPT专家")
	if err != nil || again != sessionID {
		t.Fatalf("second ensure = %q %v", again, err)
	}
	got, getErr := e.sessions.(sessionByID).Get(ctx, sessionID)
	if getErr != nil {
		t.Fatalf("session.get %s: %v", sessionID, getErr)
	}
	if got.ID != sessionID {
		t.Fatalf("got %#v", got)
	}
	if got.Title != "PPT专家" {
		t.Fatalf("title = %q", got.Title)
	}
	mapped, ok, err := roster.ThreadSession(ctx, thread.ThreadID)
	if err != nil || !ok || mapped != sessionID {
		t.Fatalf("map = %q ok=%v err=%v", mapped, ok, err)
	}
}

func TestPeopleThreadOpenEnsuresBoundSession(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "open-bound.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, projectapp.New(store, store), sessionapp.New(store, store), "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	resp := handlePeopleThreadOpen(e, ctx, bridge.Request{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		Payload: []byte(`{"peerSubjectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`),
	})
	if !resp.OK {
		t.Fatalf("open = %#v", resp)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	mapped, ok, err := roster.ThreadSession(ctx, thread.ThreadID)
	if err != nil || !ok || mapped == "" || mapped == thread.ThreadID {
		t.Fatalf("bound = %q ok=%v err=%v thread=%q", mapped, ok, err, thread.ThreadID)
	}
}

func TestPeopleBoundSessionFailureSendsSystem(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "bind-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, nil, nil, "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	reply, bindErr := e.completePeopleAgentTurn(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家",
	}, thread.ThreadID, "继续刚才的")
	if bindErr == nil {
		t.Fatal("expected bind error")
	}
	if reply != peopleBoundSessionUserError() {
		t.Fatalf("reply = %q", reply)
	}
	msgs, err := roster.ListMessages(ctx, thread.ThreadID, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range msgs {
		if item.Kind == "system" && item.Body == peopleBoundSessionUserError() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("system notice missing: %#v", msgs)
	}
}

func TestPeopleThreadOpenBindFailureSendsSystem(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "open-bind-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, nil, nil, "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	resp := handlePeopleThreadOpen(e, ctx, bridge.Request{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		Payload: []byte(`{"peerSubjectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`),
	})
	if !resp.OK {
		t.Fatalf("open = %#v", resp)
	}
	payload, _ := resp.Payload.(map[string]any)
	raw, _ := payload["messages"].([]map[string]any)
	found := false
	for _, item := range raw {
		if item["kind"] == "system" && item["body"] == peopleBoundSessionUserError() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("open bind fail must surface system notice: %#v", resp.Payload)
	}
}

func TestIsColleagueChatTitle(t *testing.T) {
	if !isColleagueChatTitle("同事 · PPT专家") || !isColleagueChatTitle("同事·Excel表格制作专家") || !isColleagueChatTitle("同事对话") {
		t.Fatal("colleague titles must be recognized")
	}
	if isColleagueChatTitle("你好月汐") || isColleagueChatTitle("写周报") {
		t.Fatal("ordinary titles must stay in 对话")
	}
}

func TestSessionListOmitsPeopleBoundAndColleagueTitledSessions(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "list-bound.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, projectapp.New(store, store), sessionapp.New(store, store), "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	boundID, err := e.ensurePeopleBoundSession(ctx, thread.ThreadID, "PPT专家")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.sessions.(sessionByID).Get(ctx, boundID)
	if err != nil {
		t.Fatal(err)
	}
	ordinary := validRequest("session.create", `{"projectId":"`+got.ProjectID+`","title":"你好月汐"}`)
	ordinary.IdempotencyKey = "ordinary-chat"
	if created := e.Handle(ctx, ordinary); !created.OK {
		t.Fatalf("ordinary create = %#v", created)
	}
	leftover := validRequest("session.create", `{"projectId":"`+got.ProjectID+`","title":"同事 · Excel表格制作专家"}`)
	leftover.IdempotencyKey = "leftover-colleague"
	if created := e.Handle(ctx, leftover); !created.OK {
		t.Fatalf("leftover create = %#v", created)
	}
	listed := e.Handle(ctx, validRequest("session.list", `{"projectId":"`+got.ProjectID+`"}`))
	if !listed.OK {
		t.Fatalf("list = %#v", listed)
	}
	body, _ := json.Marshal(listed.Payload)
	if !strings.Contains(string(body), "你好月汐") {
		t.Fatalf("ordinary chat missing: %s", body)
	}
	if strings.Contains(string(body), boundID) || strings.Contains(string(body), "PPT专家") || strings.Contains(string(body), "Excel") || strings.Contains(string(body), "同事") {
		t.Fatalf("colleague sessions leaked into 对话: %s", body)
	}
}
