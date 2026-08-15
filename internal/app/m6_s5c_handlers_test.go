// Route-level wiring coverage for the gap-10 S5C handlers: openapi.parse,
// complexity.decide and the skill.import.* pipeline through the real
// engine dispatch (schema validation + route table + SetM6GovernanceServices).
package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newS5CRouteEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "s5croute.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "s5c-route-project", "test", struct {
		Name string `json:"name"`
	}{"S5C"}, project.Project{Name: "S5C"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	if _, err := sessions.Create(context.Background(), "s5c-route-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	repo := store.AgentRuntimeRepository()
	e.SetM6GovernanceServices(m6app.NewSkillImportService(repo), m6app.NewRoutingService(repo))
	return e
}

const s5cMinimalSpec = `{"openapi":"3.0.3","info":{"title":"Pets","version":"1.0.0"},"paths":{"/pets":{"get":{"operationId":"listPets","responses":{"200":{"description":"ok"}}}}},"components":{"securitySchemes":{"api_key":{"type":"apiKey","in":"header","name":"X-Key"}}}}`

func TestOpenapiParseRoute(t *testing.T) {
	e := newS5CRouteEngine(t)
	ctx := context.Background()

	resp := e.Handle(ctx, validRequest("openapi.parse", `{"spec":`+jsonString(s5cMinimalSpec)+`}`))
	var parsed struct {
		Digest         string   `json:"digest"`
		OpenAPI        string   `json:"openapi"`
		SpecVersion    string   `json:"specVersion"`
		OperationCount int      `json:"operationCount"`
		AuthTypes      []string `json:"authTypes"`
	}
	m6Payload(t, resp, &parsed)
	if len(parsed.Digest) != 64 || parsed.OpenAPI != "3.0.3" || parsed.SpecVersion != "1.0.0" ||
		parsed.OperationCount != 1 || len(parsed.AuthTypes) != 1 || parsed.AuthTypes[0] != "apiKeyHeader" {
		t.Fatalf("parsed spec: %+v", parsed)
	}

	// A self-referencing $ref cycle answers M6-OAS-002 on the wire.
	cyclic := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},"components":{"schemas":{"a":{"$ref":"#/components/schemas/a"}}}}`
	resp = e.Handle(ctx, validRequest("openapi.parse", `{"spec":`+jsonString(cyclic)+`}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "M6-OAS-002" {
		t.Fatalf("want M6-OAS-002 for the ref cycle, got %+v", resp.Error)
	}

	// The handler-level size floor (<100 chars) answers BRIDGE_SCHEMA_INVALID.
	resp = e.Handle(ctx, validRequest("openapi.parse", `{"spec":"{}"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want BRIDGE_SCHEMA_INVALID for a tiny spec, got %+v", resp.Error)
	}
}

func TestComplexityDecideRoute(t *testing.T) {
	e := newS5CRouteEngine(t)
	ctx := context.Background()

	simple := `{"sessionId":"s-1","signals":{"messageCount":3,"estTokens":2000}}`
	var first struct {
		DecisionID string `json:"decisionId"`
		Tier       string `json:"tier"`
		RoutedPath string `json:"routedPath"`
	}
	m6Payload(t, e.Handle(ctx, validRequest("complexity.decide", simple)), &first)
	if first.DecisionID == "" || first.Tier != "simple" || first.RoutedPath != "single" {
		t.Fatalf("simple decision: %+v", first)
	}

	// Replaying the identical signal digest answers the stored decision.
	var replay struct {
		DecisionID string `json:"decisionId"`
	}
	m6Payload(t, e.Handle(ctx, validRequest("complexity.decide", simple)), &replay)
	if replay.DecisionID != first.DecisionID {
		t.Fatalf("replay must reuse %q, got %q", first.DecisionID, replay.DecisionID)
	}

	// Negative signal counters are schema-invalid.
	if resp := e.Handle(ctx, validRequest("complexity.decide", `{"sessionId":"s-1","signals":{"messageCount":-1}}`)); resp.OK {
		t.Fatal("negative counters must be rejected")
	}

	// Without the governance wiring the route degrades to a retryable
	// STORAGE_UNAVAILABLE instead of panicking on the nil service.
	bare := newS5CRouteEngine(t)
	bare.m6skills, bare.m6routing = nil, nil
	if resp := bare.Handle(ctx, validRequest("complexity.decide", simple)); resp.OK || resp.Error == nil ||
		resp.Error.Code != "STORAGE_UNAVAILABLE" || !resp.Error.Retryable {
		t.Fatalf("nil routing service: %+v", resp.Error)
	}
}

func TestSkillImportRoutePipeline(t *testing.T) {
	e := newS5CRouteEngine(t)
	ctx := context.Background()
	call := func(method, payload string) bridge.Response {
		return e.Handle(ctx, validRequest(method, payload))
	}

	// discover
	resp := call("skill.import.discover", `{"assetType":"skill","sourceUrl":"https://src.example/acme/pdf-tool","immutableCommit":"`+
		strings.Repeat("b", 40)+`","archiveHash":"`+strings.Repeat("a", 64)+`","license":"MIT","noticeRef":"NOTICE","publisher":"acme","signature":"sig-1"}`)
	var disc struct {
		CandidateID string `json:"candidateId"`
		State       string `json:"state"`
		Version     int64  `json:"version"`
	}
	m6Payload(t, resp, &disc)
	if disc.State != "discovered" || disc.Version != 1 {
		t.Fatalf("discovered: %+v", disc)
	}

	// inspect (pinned -> inspected in one call)
	resp = call("skill.import.inspect", `{"candidateId":"`+disc.CandidateID+`","expectedVersion":1,"noticeRef":"NOTICE","signature":"sig-1"}`)
	var insp struct {
		State   string `json:"state"`
		Version int64  `json:"version"`
	}
	m6Payload(t, resp, &insp)
	if insp.State != "inspected" || insp.Version != 3 {
		t.Fatalf("inspected: %+v", insp)
	}

	// submit (scanned -> evaluated -> awaiting_approval in one call);
	// scanRefs / injectionScan travel as JSON strings per the schema.
	resp = call("skill.import.submit", `{"candidateId":"`+disc.CandidateID+`","expectedVersion":`+
		jsonInt(insp.Version)+`,"scanRefs":`+jsonString(`["scan://001"]`)+`,"injectionScan":`+
		jsonString(`{"verdict":"clean"}`)+`,"evaluationId":"eval-1"}`)
	var sub struct {
		State   string `json:"state"`
		Version int64  `json:"version"`
	}
	m6Payload(t, resp, &sub)
	if sub.State != "awaiting_approval" {
		t.Fatalf("submitted: %+v", sub)
	}

	// approve materializes the skill chain from the good manifest.
	resp = call("skill.import.approve", `{"candidateId":"`+disc.CandidateID+`","expectedVersion":`+
		jsonInt(sub.Version)+`,"approval":{"by":"local"},"manifest":`+jsonString(goodSkillManifest)+`}`)
	var appr struct {
		State string `json:"state"`
	}
	m6Payload(t, resp, &appr)
	if appr.State != "approved" {
		t.Fatalf("approved: %+v", appr)
	}

	// a second candidate rejected by reason, then revoked.
	resp = call("skill.import.discover", `{"assetType":"skill","sourceUrl":"https://src.example/acme/other","immutableCommit":"`+
		strings.Repeat("c", 40)+`","archiveHash":"`+strings.Repeat("a", 64)+`","license":"MIT","publisher":"acme"}`)
	var disc2 struct {
		CandidateID string `json:"candidateId"`
	}
	m6Payload(t, resp, &disc2)
	resp = call("skill.import.reject", `{"candidateId":"`+disc2.CandidateID+`","expectedVersion":1,"reason":"not needed"}`)
	var rej struct {
		State string `json:"state"`
	}
	m6Payload(t, resp, &rej)
	if rej.State != "rejected" {
		t.Fatalf("rejected: %+v", rej)
	}
	resp = call("skill.import.revoke", `{"candidateId":"`+disc2.CandidateID+`","expectedVersion":2,"reason":"cleanup"}`)
	var rev struct {
		State string `json:"state"`
	}
	m6Payload(t, resp, &rev)
	if rev.State != "revoked" {
		t.Fatalf("revoked: %+v", rev)
	}

	// Revoking an already-revoked candidate is an idempotent replay: the
	// step short-circuits on state equality and answers success.
	resp = call("skill.import.revoke", `{"candidateId":"`+disc2.CandidateID+`","expectedVersion":1,"reason":"replay"}`)
	var replayed struct {
		State string `json:"state"`
	}
	m6Payload(t, resp, &replayed)
	if replayed.State != "revoked" {
		t.Fatalf("idempotent replay: %+v", replayed)
	}
}
