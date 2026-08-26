package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

const agentRunBudgetJSON = `"budget":{"maxModelTurns":8,"maxToolCalls":32,"maxTokens":100000,"maxCostMicros":500000,"maxWallClockSeconds":3600,"maxOutputBytes":1048576,"maxRetries":3,"maxNoProgress":5,"hardCeiling":true}`
const agentRunExpandedBudgetJSON = `"budget":{"maxModelTurns":16,"maxToolCalls":64,"maxTokens":200000,"maxCostMicros":1000000,"maxWallClockSeconds":7200,"maxOutputBytes":2097152,"maxRetries":6,"maxNoProgress":10,"hardCeiling":true}`

func agentRunEngine(t *testing.T) (*Engine, string, *storage.Store) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "agentrun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "agent-run-project", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, project.Project{Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(context.Background(), "agent-run-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	runs := agentrunapp.New(store.AgentRuntimeRepository())
	e.SetAgentRunService(runs)
	// Registered after the store's own cleanup so it runs before it: a
	// command writes its result from a goroutine that outlives the request,
	// and closing the store underneath that transaction leaves the database
	// file open — which on Windows makes t.TempDir's removal fail and the
	// test with it, long after every assertion has passed.
	t.Cleanup(runs.DrainCommands)
	return e, sess.ID, store
}

func agentRunRequest(method bridge.Method, payload, key string) bridge.Request {
	r := validRequest(string(method), payload)
	r.IdempotencyKey = key
	return r
}

func startAgentRun(t *testing.T, e *Engine, sessionID, key string) agentRunDTO {
	t.Helper()
	r := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunStart, `{"sessionId":"`+sessionID+`",`+agentRunBudgetJSON+`}`, key))
	if !r.OK {
		t.Fatalf("start: code=%s msg=%s", r.Error.Code, r.Error.Message)
	}
	body, _ := json.Marshal(r.Payload)
	var dto agentRunDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatal(err)
	}
	return dto
}

func TestAgentRunBridgeStartGetAndIdempotentReplay(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)

	started := startAgentRun(t, e, sessionID, "run-start-1")
	if started.Status != agentrun.RunRunning || started.Version != 2 || started.SessionID != sessionID {
		t.Fatalf("started=%+v", started)
	}
	if _, err := ulid.ParseStrict(started.ID); err != nil {
		t.Fatalf("run id %q: %v", started.ID, err)
	}

	// Replay with the same key and payload returns the same run.
	replay := startAgentRun(t, e, sessionID, "run-start-1")
	if replay.ID != started.ID || replay.Version != started.Version {
		t.Fatalf("replay created a new run: %+v vs %+v", replay, started)
	}

	// Same key with a different payload conflicts.
	conflict := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunStart,
		`{"sessionId":"`+sessionID+`","budget":{"maxModelTurns":9,"maxToolCalls":32,"maxTokens":100000,"maxCostMicros":500000,"maxWallClockSeconds":3600,"maxOutputBytes":1048576,"maxRetries":3,"maxNoProgress":5,"hardCeiling":true}}`, "run-start-1"))
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict=%#v", conflict)
	}

	got := e.Handle(context.Background(), validRequest(string(bridge.MethodAgentRunGet), `{"runId":"`+started.ID+`"}`))
	if !got.OK {
		t.Fatalf("get: %#v", got)
	}
	body, _ := json.Marshal(got.Payload)
	var gotDTO agentRunDTO
	if err := json.Unmarshal(body, &gotDTO); err != nil || gotDTO.ID != started.ID {
		t.Fatalf("get dto=%+v err=%v", gotDTO, err)
	}

	missing := e.Handle(context.Background(), validRequest(string(bridge.MethodAgentRunGet), `{"runId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
	if missing.OK || missing.Error.Code != "AGENT_RUN_NOT_FOUND" {
		t.Fatalf("missing=%#v", missing)
	}
}

func TestAgentRunBridgeStartValidatesInput(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)

	// Missing idempotency key.
	noKey := e.Handle(context.Background(), validRequest(string(bridge.MethodAgentRunStart), `{"sessionId":"`+sessionID+`",`+agentRunBudgetJSON+`}`))
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("noKey=%#v", noKey)
	}
	// Budget with a zero mandatory dimension is rejected.
	zeroBudget := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunStart,
		`{"sessionId":"`+sessionID+`","budget":{"maxModelTurns":0,"maxToolCalls":32,"maxTokens":100000,"maxCostMicros":500000,"maxWallClockSeconds":3600,"maxOutputBytes":1048576,"maxRetries":3,"maxNoProgress":5,"hardCeiling":true}}`, "bad-budget"))
	if zeroBudget.OK || zeroBudget.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("zeroBudget=%#v", zeroBudget)
	}
	// Unknown fields and invalid ULIDs are rejected.
	for _, raw := range []string{
		`{"sessionId":"` + sessionID + `",` + agentRunBudgetJSON + `,"extra":1}`,
		`{"sessionId":"not-a-ulid",` + agentRunBudgetJSON + `}`,
		`null`,
	} {
		bad := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunStart, raw, "bad-"+strings.ReplaceAll(raw, `"`, "")))
		if bad.OK || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("accepted %s: %#v", raw, bad)
		}
	}
}

func TestAgentRunBridgeCancelResumeAndCAS(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	started := startAgentRun(t, e, sessionID, "run-cancel-1")

	// Stale version is rejected.
	stale := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunCancel, `{"runId":"`+started.ID+`","expectedVersion":1}`, "cancel-stale"))
	if stale.OK || stale.Error.Code != "RUN_VERSION_CONFLICT" {
		t.Fatalf("stale=%#v", stale)
	}

	// running -> cancelled with the current version.
	cancel := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunCancel, `{"runId":"`+started.ID+`","expectedVersion":2}`, "cancel-1"))
	if !cancel.OK {
		t.Fatalf("cancel=%#v", cancel)
	}
	body, _ := json.Marshal(cancel.Payload)
	var cancelled agentRunDTO
	if err := json.Unmarshal(body, &cancelled); err != nil || cancelled.Status != agentrun.RunCancelled || cancelled.Version != 3 {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}

	// Terminal runs reject further transitions.
	again := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunCancel, `{"runId":"`+started.ID+`","expectedVersion":3}`, "cancel-2"))
	if again.OK || again.Error.Code != "AGENT_RUN_TERMINAL" {
		t.Fatalf("again=%#v", again)
	}

	// Generic resume must not bypass review.decide.
	second := startAgentRun(t, e, sessionID, "run-resume-1")
	repo := store.AgentRuntimeRepository()
	err := repo.Transact(context.Background(), func(tx agentrun.Tx) error {
		_, err := tx.TransitionRun(second.ID, 2, agentrun.RunPausedReview, time.Now().UTC())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	resume := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunResume, `{"runId":"`+second.ID+`","expectedVersion":3,`+agentRunExpandedBudgetJSON+`}`, "resume-1"))
	if resume.OK || resume.Error.Code != "AGENT_RUN_TRANSITION_INVALID" {
		t.Fatalf("review resume bypass=%#v", resume)
	}

	// Budget pauses are the only state generic resume may release.
	third := startAgentRun(t, e, sessionID, "run-resume-budget")
	err = repo.Transact(context.Background(), func(tx agentrun.Tx) error {
		_, err := tx.TransitionRun(third.ID, 2, agentrun.RunPausedBudget, time.Now().UTC())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Resume requires a complete expansion budget rather than silently reusing
	// the exhausted envelope.
	missingBudget := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunResume, `{"runId":"`+third.ID+`","expectedVersion":3}`, "resume-budget-missing"))
	if missingBudget.OK || missingBudget.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("missing resume budget=%#v", missingBudget)
	}

	// Resume also uses CAS before replacing the budget.
	staleResume := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunResume, `{"runId":"`+third.ID+`","expectedVersion":2,`+agentRunExpandedBudgetJSON+`}`, "resume-budget-stale"))
	if staleResume.OK || staleResume.Error.Code != "RUN_VERSION_CONFLICT" {
		t.Fatalf("stale resume=%#v", staleResume)
	}

	resume = e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunResume, `{"runId":"`+third.ID+`","expectedVersion":3,`+agentRunExpandedBudgetJSON+`}`, "resume-budget"))
	if !resume.OK {
		t.Fatalf("budget resume=%#v", resume)
	}
	body, _ = json.Marshal(resume.Payload)
	var resumed agentRunDTO
	if err := json.Unmarshal(body, &resumed); err != nil || resumed.Status != agentrun.RunRunning || resumed.Version != 5 || resumed.Budget.MaxToolCalls != 64 {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}

	// Resuming a running run is an illegal transition.
	illegal := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunResume, `{"runId":"`+third.ID+`","expectedVersion":5,`+agentRunExpandedBudgetJSON+`}`, "resume-2"))
	if illegal.OK || illegal.Error.Code != "AGENT_RUN_TRANSITION_INVALID" {
		t.Fatalf("illegal=%#v", illegal)
	}
}

func TestAgentRunBridgeReconcileResolvesPreparedEffects(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	started := startAgentRun(t, e, sessionID, "run-reconcile-1")

	repo := store.AgentRuntimeRepository()
	now := time.Now().UTC()
	digest := strings.Repeat("a", 64)
	err := repo.Transact(context.Background(), func(tx agentrun.Tx) error {
		return tx.PutEffect(agentrun.EffectJournal{
			ID:            ulid.Make().String(),
			RunID:         started.ID,
			EffectKey:     "effect-1",
			RequestDigest: digest,
			Status:        agentrun.EffectPrepared,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	reconcile := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunReconcile, `{"runId":"`+started.ID+`","expectedVersion":2}`, "reconcile-1"))
	if !reconcile.OK {
		t.Fatalf("reconcile=%#v", reconcile)
	}
	body, _ := json.Marshal(reconcile.Payload)
	var result struct {
		Run               agentRunDTO `json:"run"`
		ReconciledEffects int         `json:"reconciledEffects"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.ReconciledEffects != 1 || result.Run.ID != started.ID || result.Run.Status != agentrun.RunRunning {
		t.Fatalf("result=%+v", result)
	}

	// The prepared effect is now outcome_unknown, not retried.
	err = repo.Transact(context.Background(), func(tx agentrun.Tx) error {
		effects, err := tx.ListEffects(started.ID)
		if err != nil {
			return err
		}
		if len(effects) != 1 || effects[0].Status != agentrun.EffectOutcomeUnknown {
			t.Fatalf("effects=%+v", effects)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Replaying the same reconcile returns the recorded response and does not
	// double-count.
	replay := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunReconcile, `{"runId":"`+started.ID+`","expectedVersion":2}`, "reconcile-1"))
	if !replay.OK {
		t.Fatalf("replay=%#v", replay)
	}
	body, _ = json.Marshal(replay.Payload)
	var replayed struct {
		ReconciledEffects int `json:"reconciledEffects"`
	}
	if err := json.Unmarshal(body, &replayed); err != nil || replayed.ReconciledEffects != 1 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}

func TestCapabilityListBridgeContract(t *testing.T) {
	e, _, _ := agentRunEngine(t)

	bad := e.Handle(context.Background(), validRequest(string(bridge.MethodCapabilityList), `{"unexpected":true}`))
	if bad.OK || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad=%#v", bad)
	}

	r := e.Handle(context.Background(), validRequest(string(bridge.MethodCapabilityList), `{}`))
	if !r.OK {
		t.Fatalf("capability.list=%#v", r)
	}
	body, _ := json.Marshal(r.Payload)
	var result struct {
		Manifest agentrunapp.CapabilityManifest `json:"manifest"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	m := result.Manifest
	if m.ManifestVersion == "" || len(m.Digest) != 64 || len(m.Items) == 0 {
		t.Fatalf("manifest=%+v", m)
	}
	// M4-D shipped the read-only fs tools, M4-E the change set tools, M4-F the
	// command job tools, M4-G the web tools and M4-H the plan/evidence tools:
	// exactly those sixteen may be enabled.
	shipped := map[string]bool{
		"fs.tree": true, "fs.stat": true, "fs.read": true,
		"fs.readMany": true, "fs.glob": true, "fs.grep": true,
		"changeset.preview": true, "changeset.apply": true, "changeset.revert": true,
		"command.start": true, "command.get": true, "command.cancel": true,
		"web.fetch": true, "web.search": true,
		"run.plan.put": true, "evidence.list": true,
	}
	for _, item := range m.Items {
		if item.Enabled != shipped[item.Name] {
			t.Fatalf("tool %q enabled=%v, want %v (slice shipped set)", item.Name, item.Enabled, shipped[item.Name])
		}
		if item.RequiresReview && item.Risk == "low" {
			t.Fatalf("tool %q review/risk mismatch", item.Name)
		}
	}
}
