package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestSessionMetadataRoundTrip(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	create := validRequest("session.create", `{"projectId":"`+parent+`","title":"MRO"}`)
	create.IdempotencyKey = "mro-meta-create"
	created := e.Handle(context.Background(), create)
	if !created.OK {
		t.Fatalf("create %#v", created)
	}
	var dto struct {
		ID string `json:"id"`
	}
	body, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(body, &dto); err != nil || dto.ID == "" {
		t.Fatalf("dto %s", body)
	}
	set := validRequest("session.metadata.set", `{"sessionId":"`+dto.ID+`","mroContext":{"tailNo":"B-0000","asOf":"2026-09-03","pack":"mro.v1"}}`)
	set.IdempotencyKey = "mro-meta-set"
	got := e.Handle(context.Background(), set)
	if !got.OK {
		t.Fatalf("set %#v", got)
	}
	read := e.Handle(context.Background(), validRequest("session.metadata.get", `{"sessionId":"`+dto.ID+`"}`))
	if !read.OK {
		t.Fatalf("get %#v", read)
	}
	raw, _ := json.Marshal(read.Payload)
	if !strings.Contains(string(raw), `"tailNo":"B-0000"`) || !strings.Contains(string(raw), `"asOf":"2026-09-03"`) {
		t.Fatalf("round-trip %s", raw)
	}
}

func TestSessionMetadataRejectsMissingDate(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	create := validRequest("session.create", `{"projectId":"`+parent+`","title":"MRO"}`)
	create.IdempotencyKey = "mro-meta-bad"
	created := e.Handle(context.Background(), create)
	var dto struct {
		ID string `json:"id"`
	}
	body, _ := json.Marshal(created.Payload)
	_ = json.Unmarshal(body, &dto)
	set := validRequest("session.metadata.set", `{"sessionId":"`+dto.ID+`","mroContext":{"tailNo":"B-0000","pack":"mro.v1"}}`)
	set.IdempotencyKey = "mro-meta-bad-set"
	got := e.Handle(context.Background(), set)
	if got.OK || got.Error == nil || got.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want schema invalid, got %#v", got)
	}
}

func TestChatKBInjectUsesSessionTail(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "kb-tail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "parent-key", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, structToProject("Parent"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	kb := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	e.SetM8SliceServices(kb, nil, nil)
	ingest := NewExpertKBIngest(store.AgentRuntimeRepository(), "local-user")
	expertID := ulid.Make().String()
	path := filepath.Join(t.TempDir(), "amm.md")
	if err := os.WriteFile(path, []byte("# ATA 32\n\nGear retraction fault isolation.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Ingest(context.Background(), ExpertKBIngestInput{
		ExpertID: expertID, Path: path,
		SourceLocator: "mro://AMM/42?ata=32&status=controlled&tail=B-1000",
	}); err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(context.Background(), "sess-key", "test", map[string]string{
		"projectId": created.ID, "title": "MRO",
	}, session.Session{ProjectID: created.ID, Title: "MRO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.MergeMetadata(context.Background(), sess.ID, map[string]any{
		"mroContext": map[string]any{"tailNo": "B-0000", "asOf": "2026-09-03", "pack": "mro.v1"},
	}); err != nil {
		t.Fatal(err)
	}
	wrong := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "retraction", SessionID: sess.ID, ExpertIDs: []string{expertID},
	})
	if wrong.KBDiscarded == 0 {
		t.Fatalf("wrong tail should discard effectivity hits: cites=%d discarded=%d", len(wrong.KBCites), wrong.KBDiscarded)
	}
	if _, err := sessions.MergeMetadata(context.Background(), sess.ID, map[string]any{
		"mroContext": map[string]any{"tailNo": "B-1000", "asOf": "2026-09-03", "pack": "mro.v1"},
	}); err != nil {
		t.Fatal(err)
	}
	ok := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "retraction", SessionID: sess.ID, ExpertIDs: []string{expertID},
	})
	if len(ok.KBCites) == 0 {
		t.Fatalf("matching tail should cite: discarded=%d evidence=%q", ok.KBDiscarded, joinEvidence(ok))
	}
}
