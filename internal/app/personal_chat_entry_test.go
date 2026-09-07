package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// This fixture is also asserted against the real renderer's
// ensurePersonalProject request. A permissive project bridge mock used to hide
// the rejection of this name-only request by the production handler.
func personalChatCreateRequest(t *testing.T) bridge.Request {
	t.Helper()
	raw, err := os.ReadFile("testdata/personal_chat_project.json")
	if err != nil {
		t.Fatal(err)
	}
	r := validRequest("project.create", string(raw))
	r.IdempotencyKey = "personal-chat-bootstrap"
	return r
}

func TestPersonalChatEntryCreatesTypedAndVoiceSessionsWithoutDeletingData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "entry.db")
	store, err := storage.OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	newEngine := func(store *storage.Store) *Engine {
		messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatal(err)
		}
		return NewEngineWithMessages(providerapp.New(store, store), projectapp.New(store, store), sessionapp.New(store, store), messages, "test", nil)
	}
	e := newEngine(store)
	// Preserve an existing business project while bootstrapping the previously
	// missing chat container. No production profile is opened by this test.
	oldRequest := validRequest("project.create", validProjectCreateJSON)
	oldRequest.IdempotencyKey = "existing-business-project"
	oldResponse := e.Handle(ctx, oldRequest)
	var old projectDTO
	if !oldResponse.OK || decodeResponsePayload(oldResponse.Payload, &old) != nil {
		t.Fatalf("existing project: %+v", oldResponse)
	}
	r := personalChatCreateRequest(t)
	created := e.Handle(ctx, r)
	if !created.OK {
		t.Fatalf("renderer personal chat request rejected: %+v", created.Error)
	}
	var parent projectDTO
	if err := decodeResponsePayload(created.Payload, &parent); err != nil {
		t.Fatal(err)
	}
	if parent.Name != "\u2063月汐·普通对话" || parent.Type != project.TypeImplementation || parent.Client != "" || parent.PlanStart != "" || parent.PlanEnd != "" {
		t.Fatalf("chat container must not invent business fields: %+v", parent)
	}
	if replay := e.Handle(ctx, r); !replay.OK || !reflect.DeepEqual(replay.Payload, created.Payload) {
		t.Fatalf("create replay: %+v", replay)
	}
	var ids []string
	for i, title := range []string{"你好", "月伴对话"} {
		sessionRequest := validRequest("session.create", fmt.Sprintf(`{"projectId":%q,"title":%q}`, parent.ID, title))
		sessionRequest.IdempotencyKey = fmt.Sprintf("entry-session-%d", i)
		response := e.Handle(ctx, sessionRequest)
		var item sessionDTO
		if !response.OK || decodeResponsePayload(response.Payload, &item) != nil {
			t.Fatalf("%s entry: %+v", title, response)
		}
		ids = append(ids, item.ID)
		appendRequest := validRequest("message.append", fmt.Sprintf(`{"sessionId":%q,"text":%q}`, item.ID, "保留的历史："+title))
		appendRequest.IdempotencyKey = fmt.Sprintf("entry-message-%d", i)
		if response := e.Handle(ctx, appendRequest); !response.OK {
			t.Fatalf("persist entry history: %+v", response.Error)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	e = newEngine(store)
	listed := e.Handle(ctx, validRequest("project.list", `{}`))
	var projects struct {
		Items []projectDTO `json:"items"`
	}
	if !listed.OK || decodeResponsePayload(listed.Payload, &projects) != nil || len(projects.Items) != 2 {
		t.Fatalf("projects after reopen: %+v", listed)
	}
	if !reflect.DeepEqual(projects.Items[0], old) {
		t.Fatalf("existing project changed: %+v", projects.Items[0])
	}
	for i, id := range ids {
		response := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, id)))
		var page messageapp.Page
		if !response.OK || decodeResponsePayload(response.Payload, &page) != nil || len(page.Items) != 1 {
			t.Fatalf("history %d after reopen: %+v", i, response)
		}
	}
	// The same frontend request remains replayable after an engine restart.
	if replay := e.Handle(ctx, personalChatCreateRequest(t)); !replay.OK || !reflect.DeepEqual(replay.Payload, created.Payload) {
		t.Fatalf("restarted create replay: %+v", replay)
	}
}

func TestPersonalChatEntryDoesNotRelaxBusinessProjectValidation(t *testing.T) {
	e, _, _ := projectUpgradeEngine(t)
	for _, payload := range []string{
		`{"name":"Ordinary project"}`,
		`{"name":"\u2063Some other hidden name"}`,
		`{"name":"\u2063月汐·普通对话 ","type":"implementation"}`,
		`{"name":"\u2063月汐·普通对话","type":"operations"}`,
		`{"name":"\u2063月汐·普通对话","client":"incomplete business form"}`,
	} {
		r := validRequest("project.create", payload)
		r.IdempotencyKey = "invalid-personal-shape"
		if response := e.Handle(context.Background(), r); response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("incomplete business form accepted: %s: %+v", payload, response)
		}
	}
	missingKey := personalChatCreateRequest(t)
	missingKey.IdempotencyKey = ""
	if response := e.Handle(context.Background(), missingKey); response.OK {
		t.Fatalf("personal creation bypassed idempotency: %+v", response)
	}
	for _, field := range []string{"orgId", "status"} {
		r := personalChatCreateRequest(t)
		var payload map[string]any
		if err := json.Unmarshal(r.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		payload[field] = "forged"
		r.Payload, _ = json.Marshal(payload)
		if response := e.Handle(context.Background(), r); response.OK {
			t.Fatalf("personal creation accepted forged %s: %+v", field, response)
		}
	}
}
