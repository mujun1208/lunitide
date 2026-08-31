package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newExpertSkillsEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "expert-skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	e := NewEngine(nil, "test")
	e.SetM8ExpertService(svc)
	return e
}

func TestExpertSkillsGetSetThroughBridge(t *testing.T) {
	e := newExpertSkillsEngine(t)
	ctx := context.Background()
	created := e.Handle(ctx, nominationRequest("expert.create", `{"source":"local","frontmatter":{"name":"安全顾问","division":"security","description":"边界守卫","semver":"1.0.0"},"sixSection":{"identity":"i","mission":"m","rules":"r","workflow":"w","deliverableTemplate":"d","successMetrics":"s"},"requestId":"req-skills-1","skillKeys":["web-researcher"]}`))
	if !created.OK {
		t.Fatalf("expert.create: %+v", created.Error)
	}
	var createdPayload struct {
		ExpertID string `json:"expertId"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	got := e.Handle(ctx, nominationRequest("expert.skills.get", `{"expertId":"`+createdPayload.ExpertID+`"}`))
	if !got.OK {
		t.Fatalf("skills.get: %+v", got.Error)
	}
	if string(mustJSON(got.Payload)) == "" || !jsonHasSkillKey(mustJSON(got.Payload), "web-researcher") {
		t.Fatalf("skills.get payload = %s", mustJSON(got.Payload))
	}
	setReq := nominationRequest("expert.skills.set", `{"expertId":"`+createdPayload.ExpertID+`","skillKeys":["slide-builder"]}`)
	setReq.IdempotencyKey = "expert-skills-set-1"
	set := e.Handle(ctx, setReq)
	if !set.OK {
		t.Fatalf("skills.set: %+v", set.Error)
	}
	if !jsonHasSkillKey(mustJSON(set.Payload), "slide-builder") {
		t.Fatalf("skills.set payload = %s", mustJSON(set.Payload))
	}
}

func jsonHasSkillKey(raw []byte, key string) bool {
	var payload struct {
		SkillKeys []string `json:"skillKeys"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	for _, item := range payload.SkillKeys {
		if item == key {
			return true
		}
	}
	return false
}
