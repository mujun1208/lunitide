package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// M4-H run.plan.put/evidence.list bridge tests. They assert the versioned
// plan projection (OCC CAS, canonical digest convergence), the single
// transaction commit (plan + RunPlanPutCompleted event + run.plan.updated
// audit + idempotency record), idempotent replay, and the read-only evidence
// projection over records written by the web flow.

// runPlanEngine mirrors agentRunEngine but also returns the database path so
// tests can verify the audit trail directly.
func runPlanEngine(t *testing.T) (*Engine, string, *storage.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runplan.db")
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "run-plan-project", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, project.Project{Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(context.Background(), "run-plan-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	e.SetAgentRunService(agentrunapp.New(store.AgentRuntimeRepository()))
	return e, sess.ID, store, path
}

func planCall(e *Engine, method bridge.Method, payload, key string) bridge.Response {
	return e.Handle(context.Background(), agentRunRequest(method, payload, key))
}

type runPlanPutOut struct {
	Plan struct {
		ID         string          `json:"id"`
		RunID      string          `json:"runId"`
		PlanDigest string          `json:"planDigest"`
		Plan       json.RawMessage `json:"plan"`
		Version    int64           `json:"version"`
	} `json:"plan"`
}

func decodePlanPut(t *testing.T, res bridge.Response) runPlanPutOut {
	t.Helper()
	body, _ := json.Marshal(res.Payload)
	var out runPlanPutOut
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func storedRunPlan(t *testing.T, store *storage.Store, runID string) agentrun.RunPlan {
	t.Helper()
	var plan agentrun.RunPlan
	err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		p, err := tx.GetRunPlan(runID)
		plan = p
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func auditActionCount(t *testing.T, path, action string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM audit_events WHERE action=?`, action).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRunPlanPutLifecycleAndCanonicalConvergence(t *testing.T) {
	e, sessionID, store, path := runPlanEngine(t)
	run := startAgentRun(t, e, sessionID, "plan-life-run")

	// Create: expectedVersion 0 commits version 1.
	first := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"summarize","steps":["read","write"]}}`, "plan-life-1")
	if !first.OK {
		t.Fatalf("first: code=%s msg=%s", first.Error.Code, first.Error.Message)
	}
	out := decodePlanPut(t, first)
	if out.Plan.RunID != run.ID || out.Plan.Version != 1 || len(out.Plan.PlanDigest) != 64 {
		t.Fatalf("out=%+v", out.Plan)
	}
	if string(out.Plan.Plan) != `{"goal":"summarize","steps":["read","write"]}` {
		t.Fatalf("plan content is not canonical: %s", out.Plan.Plan)
	}
	stored := storedRunPlan(t, store, run.ID)
	if stored.ID != out.Plan.ID || stored.PlanDigest != out.Plan.PlanDigest || stored.Version != 1 {
		t.Fatalf("stored=%+v", stored)
	}
	events := runEventsOfType(t, store, run.ID, agentrun.EventRunPlanPutCompleted)
	if len(events) != 1 || !strings.Contains(string(events[0].Payload), out.Plan.PlanDigest) {
		t.Fatalf("events=%+v", events)
	}

	// Update with a semantically equal plan whose raw key order differs:
	// canonical encoding converges on the same digest while the version moves.
	second := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":1,"plan":{"steps":["read","write"],"goal":"summarize"}}`, "plan-life-2")
	if !second.OK {
		t.Fatalf("second: code=%s msg=%s", second.Error.Code, second.Error.Message)
	}
	out2 := decodePlanPut(t, second)
	if out2.Plan.Version != 2 || out2.Plan.PlanDigest != out.Plan.PlanDigest || out2.Plan.ID != out.Plan.ID {
		t.Fatalf("convergence failed: first=%+v second=%+v", out.Plan, out2.Plan)
	}
	if got := runEventsOfType(t, store, run.ID, agentrun.EventRunPlanPutCompleted); len(got) != 2 {
		t.Fatalf("events=%+v", got)
	}
	if got := auditActionCount(t, path, "run.plan.updated"); got != 2 {
		t.Fatalf("audit run.plan.updated count=%d, want 2", got)
	}
}

func TestRunPlanPutVersionCAS(t *testing.T) {
	e, sessionID, _, _ := runPlanEngine(t)
	run := startAgentRun(t, e, sessionID, "plan-cas-run")

	// Create with a non-zero expectedVersion is a conflict.
	create := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":3,"plan":{"goal":"g"}}`, "plan-cas-create")
	if create.OK || create.Error.Code != "RUN_VERSION_CONFLICT" {
		t.Fatalf("create=%#v", create)
	}

	ok := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"g"}}`, "plan-cas-ok")
	if !ok.OK || decodePlanPut(t, ok).Plan.Version != 1 {
		t.Fatalf("ok=%#v", ok)
	}

	// Stale expectedVersion is rejected; nothing changes.
	stale := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"h"}}`, "plan-cas-stale")
	if stale.OK || stale.Error.Code != "RUN_VERSION_CONFLICT" {
		t.Fatalf("stale=%#v", stale)
	}

	next := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":1,"plan":{"goal":"h"}}`, "plan-cas-next")
	if !next.OK || decodePlanPut(t, next).Plan.Version != 2 {
		t.Fatalf("next=%#v", next)
	}
}

func TestRunPlanPutIdempotentReplayAndConflict(t *testing.T) {
	e, sessionID, store, path := runPlanEngine(t)
	run := startAgentRun(t, e, sessionID, "plan-idem-run")
	payload := `{"runId":"` + run.ID + `","expectedVersion":0,"plan":{"goal":"g"}}`

	first := planCall(e, bridge.MethodRunPlanPut, payload, "plan-idem-1")
	if !first.OK {
		t.Fatalf("first: code=%s msg=%s", first.Error.Code, first.Error.Message)
	}
	replay := planCall(e, bridge.MethodRunPlanPut, payload, "plan-idem-1")
	if !replay.OK {
		t.Fatalf("replay: code=%s msg=%s", replay.Error.Code, replay.Error.Message)
	}
	if decodePlanPut(t, first).Plan.ID != decodePlanPut(t, replay).Plan.ID ||
		decodePlanPut(t, replay).Plan.Version != 1 {
		t.Fatal("replay must return the committed plan without a new version")
	}
	if got := runEventsOfType(t, store, run.ID, agentrun.EventRunPlanPutCompleted); len(got) != 1 {
		t.Fatalf("replay emitted a duplicate event: %+v", got)
	}
	if got := auditActionCount(t, path, "run.plan.updated"); got != 1 {
		t.Fatalf("replay duplicated audit: count=%d", got)
	}

	conflict := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"other"}}`, "plan-idem-1")
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict=%#v", conflict)
	}
}

func TestRunPlanPutValidationAndStateGuard(t *testing.T) {
	e, sessionID, store, _ := runPlanEngine(t)
	run := startAgentRun(t, e, sessionID, "plan-val-run")

	noKey := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"g"}}`, "")
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("noKey=%#v", noKey)
	}
	for name, payload := range map[string]string{
		"bad run id":        `{"runId":"not-a-ulid","expectedVersion":0,"plan":{"goal":"g"}}`,
		"negative version":  `{"runId":"` + run.ID + `","expectedVersion":-1,"plan":{"goal":"g"}}`,
		"plan not an object": `{"runId":"` + run.ID + `","expectedVersion":0,"plan":"text"}`,
		"plan array":         `{"runId":"` + run.ID + `","expectedVersion":0,"plan":[1,2]}`,
		"missing plan":       `{"runId":"` + run.ID + `","expectedVersion":0}`,
		"oversize plan":      `{"runId":"` + run.ID + `","expectedVersion":0,"plan":{"pad":"` + strings.Repeat("x", 70*1024) + `"}}`,
	} {
		res := planCall(e, bridge.MethodRunPlanPut, payload, "plan-val-"+strings.ReplaceAll(name, " ", "-"))
		if res.OK || res.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Errorf("%s=%#v", name, res)
		}
	}
	if got := runEventsOfType(t, store, run.ID, agentrun.EventRunPlanPutCompleted); len(got) != 0 {
		t.Fatalf("rejected puts recorded events: %+v", got)
	}

	// A non-running run rejects plan writes.
	cancel := planCall(e, bridge.MethodAgentRunCancel,
		`{"runId":"`+run.ID+`","expectedVersion":2}`, "plan-val-cancel")
	if !cancel.OK {
		t.Fatalf("cancel=%#v", cancel)
	}
	nonRunning := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"`+run.ID+`","expectedVersion":0,"plan":{"goal":"g"}}`, "plan-val-state")
	if nonRunning.OK || nonRunning.Error.Code != "AGENT_RUN_TRANSITION_INVALID" {
		t.Fatalf("nonRunning=%#v", nonRunning)
	}

	missing := planCall(e, bridge.MethodRunPlanPut,
		`{"runId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","expectedVersion":0,"plan":{"goal":"g"}}`, "plan-val-missing")
	if missing.OK || missing.Error.Code != "AGENT_RUN_NOT_FOUND" {
		t.Fatalf("missing=%#v", missing)
	}
}

type evidenceListOut struct {
	RunID    string `json:"runId"`
	Evidence []struct {
		ID            string `json:"id"`
		RunID         string `json:"runId"`
		Kind          string `json:"kind"`
		SourceURI     string `json:"sourceUri"`
		ContentDigest string `json:"contentDigest"`
	} `json:"evidence"`
}

func decodeEvidenceList(t *testing.T, res bridge.Response) evidenceListOut {
	t.Helper()
	body, _ := json.Marshal(res.Payload)
	var out evidenceListOut
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEvidenceListBridgeProjection(t *testing.T) {
	e, sessionID, _, _ := runPlanEngine(t)
	e.agentRuns.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{
			FinalURL: rawURL, Status: 200, ContentType: "text/plain", Body: []byte("payload"),
		}, nil
	})
	run := startAgentRun(t, e, sessionID, "ev-list-run")

	// Empty projection before any evidence is recorded.
	empty := planCall(e, bridge.MethodEvidenceList, `{"runId":"`+run.ID+`"}`, "")
	if !empty.OK {
		t.Fatalf("empty: code=%s msg=%s", empty.Error.Code, empty.Error.Message)
	}
	if got := decodeEvidenceList(t, empty); got.RunID != run.ID || len(got.Evidence) != 0 {
		t.Fatalf("empty=%+v", got)
	}

	// Record two evidence rows through the web flow, then list them in order.
	for i, url := range []string{"https://example.com/a", "https://example.com/b"} {
		res := planCall(e, bridge.MethodWebFetch, `{"runId":"`+run.ID+`","url":"`+url+`"}`, "ev-list-fetch-"+string(rune('1'+i)))
		if !res.OK {
			t.Fatalf("fetch %s: code=%s msg=%s", url, res.Error.Code, res.Error.Message)
		}
	}
	listed := planCall(e, bridge.MethodEvidenceList, `{"runId":"`+run.ID+`"}`, "")
	if !listed.OK {
		t.Fatalf("listed: code=%s msg=%s", listed.Error.Code, listed.Error.Message)
	}
	out := decodeEvidenceList(t, listed)
	if out.RunID != run.ID || len(out.Evidence) != 2 {
		t.Fatalf("out=%+v", out)
	}
	if out.Evidence[0].SourceURI != "https://example.com/a" || out.Evidence[1].SourceURI != "https://example.com/b" {
		t.Fatalf("evidence order=%+v", out.Evidence)
	}
	for _, ev := range out.Evidence {
		if ev.RunID != run.ID || ev.Kind != "web.fetch" || len(ev.ContentDigest) != 64 {
			t.Fatalf("evidence=%+v", ev)
		}
	}

	// Evidence stays readable after the run terminates.
	cancel := planCall(e, bridge.MethodAgentRunCancel, `{"runId":"`+run.ID+`","expectedVersion":2}`, "ev-list-cancel")
	if !cancel.OK {
		t.Fatalf("cancel=%#v", cancel)
	}
	after := planCall(e, bridge.MethodEvidenceList, `{"runId":"`+run.ID+`"}`, "")
	if !after.OK || len(decodeEvidenceList(t, after).Evidence) != 2 {
		t.Fatalf("after=%#v", after)
	}
}

func TestEvidenceListValidation(t *testing.T) {
	e, sessionID, _, _ := runPlanEngine(t)
	run := startAgentRun(t, e, sessionID, "ev-val-run")

	bad := planCall(e, bridge.MethodEvidenceList, `{"runId":"bad"}`, "")
	if bad.OK || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad=%#v", bad)
	}
	missing := planCall(e, bridge.MethodEvidenceList, `{"runId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`, "")
	if missing.OK || missing.Error.Code != "AGENT_RUN_NOT_FOUND" {
		t.Fatalf("missing=%#v", missing)
	}
	// Isolation: another run's evidence is not visible.
	other := startAgentRun(t, e, sessionID, "ev-val-other")
	e.agentRuns.SetWebFetcher(fetcherReturning(networkpolicy.FetchResult{
		FinalURL: "https://example.com/", Status: 200, ContentType: "text/plain", Body: []byte("x"),
	}, nil))
	if res := planCall(e, bridge.MethodWebFetch, `{"runId":"`+other.ID+`","url":"https://example.com/"}`, "ev-val-fetch"); !res.OK {
		t.Fatalf("fetch=%#v", res)
	}
	listed := planCall(e, bridge.MethodEvidenceList, `{"runId":"`+run.ID+`"}`, "")
	if !listed.OK || len(decodeEvidenceList(t, listed).Evidence) != 0 {
		t.Fatalf("cross-run leakage: %#v", listed)
	}
}
