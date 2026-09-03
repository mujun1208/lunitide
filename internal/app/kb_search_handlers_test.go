package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newKBSearchEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "kb-search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	kb := m8app.NewKBService(repo, "local-user")
	e.SetM8SliceServices(kb, nil, nil)
	e.SetExpertGrowthService(m8app.NewGrowthService(repo))
	return e
}

func TestKBSearchHandlerEmptyQueryFails(t *testing.T) {
	e := newKBSearchEngine(t)
	resp := e.Handle(context.Background(), nominationRequest("kb.search", `{"expertId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","query":""}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("empty query = %+v", resp)
	}
}

func TestKBSearchHandlerMissingCollection(t *testing.T) {
	e := newKBSearchEngine(t)
	id := ulid.Make().String()
	resp := e.Handle(context.Background(), nominationRequest("kb.search", `{"expertId":"`+id+`","query":"retraction"}`))
	if !resp.OK {
		t.Fatalf("missing collection should be 200: %+v", resp.Error)
	}
	var body struct {
		Explanation struct {
			Missing bool `json:"missing"`
		} `json:"explanation"`
	}
	if err := json.Unmarshal(mustJSON(resp.Payload), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Explanation.Missing {
		t.Fatalf("want missing: %s", resp.Payload)
	}
}

func TestExpertKnowledgeGetMissingIsOK(t *testing.T) {
	e := newKBSearchEngine(t)
	id := ulid.Make().String()
	resp := e.Handle(context.Background(), nominationRequest("expert.knowledge.get", `{"expertId":"`+id+`"}`))
	if !resp.OK {
		t.Fatalf("knowledge.get = %+v", resp.Error)
	}
	var body struct {
		CollectionID string `json:"collectionId"`
		Missing      bool   `json:"missing"`
	}
	if err := json.Unmarshal(mustJSON(resp.Payload), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Missing || body.CollectionID != "" {
		t.Fatalf("empty expert must be missing without collectionId: %+v", body)
	}
}
