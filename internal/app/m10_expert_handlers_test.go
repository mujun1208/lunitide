// M10 expert scenario-card bridge tests: the full journey over the engine
// handle path — create -> list -> delete (soft archive) -> duplicate and
// validation guards. Also verifies the M10-EX-001~003 error family.
package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newScenarioEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m10scen.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	e.SetM8ExpertService(m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{}))
	e.SetM10ScenarioService(m8app.NewScenarioService(repo))
	return e
}

// createScenarioExpert seeds one local expert and answers its id.
func createScenarioExpert(t *testing.T, e *Engine, ctx context.Context) string {
	t.Helper()
	created := e.Handle(ctx, nominationRequest("expert.create", `{"source":"local","frontmatter":{"name":"Database Optimizer","division":"engineering","description":"Index and query tuning","semver":"1.0.0"},"sixSection":{"identity":"i","mission":"m","rules":"r","workflow":"w","deliverableTemplate":"d","successMetrics":"s"},"requestId":"req-scen-1"}`))
	if !created.OK {
		t.Fatalf("expert.create failed: %+v", created.Error)
	}
	var payload struct {
		ExpertID string `json:"expertId"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.ExpertID
}

func TestScenarioLifecycleThroughBridge(t *testing.T) {
	e := newScenarioEngine(t)
	ctx := context.Background()
	expertID := createScenarioExpert(t, e, ctx)

	created := e.Handle(ctx, nominationRequest("expert.scenario.create", `{"expertId":"`+expertID+`","title":"数据库慢查询处置","summary":"针对慢查询的索引与执行计划处置剧本","phaseKey":"DEVELOPMENT_CHANGE","scenario":{"steps":["定位慢SQL","EXPLAIN 分析","索引建议"],"slowThresholdMs":200}}`))
	if !created.OK {
		t.Fatalf("scenario.create failed: %+v", created.Error)
	}
	var createdPayload struct {
		ScenarioCardID string `json:"scenarioCardId"`
		ExpertID       string `json:"expertId"`
		Title          string `json:"title"`
		PhaseKey       string `json:"phaseKey"`
		Digest         string `json:"digest"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	if len(createdPayload.ScenarioCardID) != 26 || len(createdPayload.Digest) != 64 ||
		createdPayload.Title != "数据库慢查询处置" || createdPayload.PhaseKey != "DEVELOPMENT_CHANGE" {
		t.Fatalf("scenario.create result invalid: %+v", createdPayload)
	}

	listed := e.Handle(ctx, nominationRequest("expert.scenario.list", `{"expertId":"`+expertID+`","state":"active"}`))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), "数据库慢查询处置") {
		t.Fatalf("scenario.list missing entry: %+v", listed.Payload)
	}

	// Duplicate title answers M10-EX-002.
	duplicate := e.Handle(ctx, nominationRequest("expert.scenario.create", `{"expertId":"`+expertID+`","title":"数据库慢查询处置","summary":"s","phaseKey":"ARCHITECTURE_PLAN","scenario":{"a":1}}`))
	if duplicate.OK || duplicate.Error.Code != "M10-EX-002" {
		t.Fatalf("duplicate = %+v, want M10-EX-002", duplicate.Error)
	}

	// Delete is the guarded soft archive.
	deleted := e.Handle(ctx, nominationRequest("expert.scenario.delete", `{"scenarioCardId":"`+createdPayload.ScenarioCardID+`","actor":"local-user"}`))
	if !deleted.OK {
		t.Fatalf("scenario.delete failed: %+v", deleted.Error)
	}
	archived := e.Handle(ctx, nominationRequest("expert.scenario.list", `{"expertId":"`+expertID+`","state":"archived"}`))
	if !archived.OK || !strings.Contains(string(mustJSON(archived.Payload)), createdPayload.ScenarioCardID) {
		t.Fatalf("archived list missing entry: %+v", archived.Payload)
	}
	active := e.Handle(ctx, nominationRequest("expert.scenario.list", `{"expertId":"`+expertID+`","state":"active"}`))
	if !active.OK || strings.Contains(string(mustJSON(active.Payload)), createdPayload.ScenarioCardID) {
		t.Fatalf("active list must not contain archived card: %+v", active.Payload)
	}

	// Archived cards answer M10-EX-001 on a second delete.
	again := e.Handle(ctx, nominationRequest("expert.scenario.delete", `{"scenarioCardId":"`+createdPayload.ScenarioCardID+`"}`))
	if again.OK || again.Error.Code != "M10-EX-001" {
		t.Fatalf("second delete = %+v, want M10-EX-001", again.Error)
	}
}

func TestScenarioBridgeValidation(t *testing.T) {
	e := newScenarioEngine(t)
	ctx := context.Background()
	expertID := createScenarioExpert(t, e, ctx)
	cases := []struct {
		method, payload string
	}{
		{"expert.scenario.create", `{"expertId":"not-a-ulid","title":"t","summary":"s","phaseKey":"ARCHITECTURE_PLAN","scenario":{"a":1}}`},
		{"expert.scenario.create", `{"expertId":"` + expertID + `","title":"","summary":"s","phaseKey":"ARCHITECTURE_PLAN","scenario":{"a":1}}`},
		{"expert.scenario.create", `{"expertId":"` + expertID + `","title":"t","summary":"s","phaseKey":"PHASE_TEN","scenario":{"a":1}}`},
		{"expert.scenario.list", `{"expertId":"not-a-ulid"}`},
		{"expert.scenario.list", `{"expertId":"` + expertID + `","state":"deleted"}`},
		{"expert.scenario.delete", `{"scenarioCardId":"not-a-ulid"}`},
	}
	for _, tc := range cases {
		resp := e.Handle(ctx, nominationRequest(tc.method, tc.payload))
		if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%s %s = %+v, want BRIDGE_SCHEMA_INVALID", tc.method, tc.payload, resp.Error)
		}
	}
	// Unknown expert answers BRIDGE_NOT_FOUND through the FR-19 family.
	resp := e.Handle(ctx, nominationRequest("expert.scenario.create", `{"expertId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","title":"t","summary":"s","phaseKey":"ARCHITECTURE_PLAN","scenario":{"a":1}}`))
	if resp.OK || resp.Error.Code != "BRIDGE_NOT_FOUND" {
		t.Fatalf("unknown expert = %+v, want BRIDGE_NOT_FOUND", resp.Error)
	}
}

func TestScenarioServiceUnavailable(t *testing.T) {
	e := NewEngine(nil, "test")
	ctx := context.Background()
	cases := map[string]string{
		"expert.scenario.create": `{"expertId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","title":"t","summary":"s","phaseKey":"ARCHITECTURE_PLAN","scenario":{"a":1}}`,
		"expert.scenario.list":   `{"expertId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
		"expert.scenario.delete": `{"scenarioCardId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
	}
	for method, payload := range cases {
		resp := e.Handle(ctx, nominationRequest(method, payload))
		if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
			t.Fatalf("%s unwired = %+v, want STORAGE_UNAVAILABLE", method, resp.Error)
		}
	}
}
