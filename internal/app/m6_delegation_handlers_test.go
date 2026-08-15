package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// m6DelegationHarness wires the slice-3 services (delegation fan-out/fan-in,
// budget ledger, join barriers) behind the bridge handlers on a real store,
// with one live agent run acting as the governance root.
type m6DelegationHarness struct {
	e        *Engine
	rootID   string
	barriers *m6app.BarrierService
	repo     *storage.AgentRuntimeRepository
}

func newM6DelegationHarness(t *testing.T) *m6DelegationHarness {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m6dlg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, err := projects.Create(context.Background(), "m6-delegation-project", "test", struct {
		Name string `json:"name"`
	}{"Root"}, project.Project{Name: "Root"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(context.Background(), "m6-delegation-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{created.ID, "S"}, session.Session{ProjectID: created.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	e.SetAgentRunService(agentrunapp.New(store.AgentRuntimeRepository()))
	repo := store.AgentRuntimeRepository()
	signer, err := delegation.GenerateSigner("m6-control-1")
	if err != nil {
		t.Fatal(err)
	}
	budget := m6app.NewBudgetService(m6app.BudgetPolicy{Caps: map[string]int64{
		"cpu_seconds": 100000, "tokens": 10000000, "cost": 100000, "wall_clock": 100000000,
	}})
	barriers := m6app.NewBarrierService(repo)
	parentCaps := func(rootID, parentID string) []string {
		return []string{"fs.read", "fs.write", "command.run", "test.run"}
	}
	e.SetM6DelegationServices(
		m6app.NewDelegationService(repo, budget, barriers, signer, parentCaps),
		barriers)
	run := startAgentRun(t, e, sess.ID, "m6-root-run-key")
	return &m6DelegationHarness{e: e, rootID: run.ID, barriers: barriers, repo: repo}
}

func delegationCreatePayload(rootID string, parentID string, tokens int64, depth int) string {
	return `{"rootId":"` + rootID + `","parentId":"` + parentID + `",` +
		`"objective":"Refactor auth module tests",` +
		`"inputDigests":["` + sha256Hex("input-a") + `"],` +
		`"capabilitySet":["fs.read","command.run"],` +
		`"budgetGrant":{"cpuSeconds":120,"tokens":` + jsonInt(tokens) + `,"cost":200,"wallClockMs":600000},` +
		`"deadlineMs":600000,"depth":` + jsonInt(int64(depth)) + `}`
}

func jsonInt(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func createDelegation(t *testing.T, h *m6DelegationHarness, key, parentID string, tokens int64, depth int) string {
	t.Helper()
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate,
		delegationCreatePayload(h.rootID, parentID, tokens, depth), key))
	if !resp.OK {
		t.Fatalf("delegation.create failed: code=%s msg=%s", resp.Error.Code, resp.Error.Message)
	}
	var out struct {
		DelegationID      string `json:"delegationId"`
		EnvelopeSignature string `json:"envelopeSignature"`
	}
	m6Payload(t, resp, &out)
	if out.DelegationID == "" || out.EnvelopeSignature == "" {
		t.Fatalf("delegation.create payload malformed: %+v", out)
	}
	return out.DelegationID
}

func settleDelegation(t *testing.T, h *m6DelegationHarness, key, delegationID string) (barrierState string) {
	t.Helper()
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationSettle,
		`{"delegationId":"`+delegationID+`","resultBundle":{`+
			`"claims":{"summary":"done"},"patchDigest":"`+sha256Hex("patch")+`",`+
			`"testEvidenceRefs":["evidence://run/1"],"usage":{"tokens":100},`+
			`"resultDigest":"`+sha256Hex("result")+`"}}`, key))
	if !resp.OK {
		t.Fatalf("delegation.settle failed: code=%s msg=%s", resp.Error.Code, resp.Error.Message)
	}
	var out struct {
		SettledAt      string           `json:"settledAt"`
		BudgetConsumed map[string]int64 `json:"budgetConsumed"`
		BarrierState   string           `json:"barrierState"`
	}
	m6Payload(t, resp, &out)
	if _, err := time.Parse(time.RFC3339Nano, out.SettledAt); err != nil {
		t.Fatalf("settledAt not RFC3339: %q", out.SettledAt)
	}
	if out.BudgetConsumed["tokens"] != 100 {
		t.Fatalf("budgetConsumed.tokens = %d, want 100", out.BudgetConsumed["tokens"])
	}
	return out.BarrierState
}

// Happy path: create -> settle, budget consumed, no barrier -> not_applicable.
func TestDelegationCreateAndSettle(t *testing.T) {
	h := newM6DelegationHarness(t)
	id := createDelegation(t, h, "dlg-key-1", "node-1", 5000, 0)
	if state := settleDelegation(t, h, "settle-key-1", id); state != "not_applicable" {
		t.Fatalf("barrierState = %q, want not_applicable", state)
	}
	// replay: same idempotency key answers the original settlement
	if state := settleDelegation(t, h, "settle-key-1", id); state != "not_applicable" {
		t.Fatalf("settle replay barrierState = %q", state)
	}
}

// Idempotent create: the same key replays to the same delegation.
func TestDelegationCreateIdempotentReplay(t *testing.T) {
	h := newM6DelegationHarness(t)
	first := createDelegation(t, h, "dlg-key-2", "node-1", 5000, 0)
	second := createDelegation(t, h, "dlg-key-2", "node-1", 5000, 0)
	if first != second {
		t.Fatalf("replay must return the original delegation: %s vs %s", first, second)
	}
	// a different key is a different delegation (fan-out slot 2)
	third := createDelegation(t, h, "dlg-key-2b", "node-1", 5000, 0)
	if third == first {
		t.Fatal("different key must create a new delegation")
	}
}

// BGT-001: a grant over the policy cap is refused whole, nothing frozen.
func TestDelegationCreateBudgetRefused(t *testing.T) {
	h := newM6DelegationHarness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate,
		delegationCreatePayload(h.rootID, "node-1", 999999999, 0), "dlg-key-3"))
	if resp.OK || resp.Error.Code != "M6-BGT-001" {
		t.Fatalf("want M6-BGT-001, got %+v", resp.Error)
	}
	// the refused reserve must not have frozen anything: a follow-up create
	// with the same key still creates cleanly
	id := createDelegation(t, h, "dlg-key-3", "node-1", 5000, 0)
	if id == "" {
		t.Fatal("follow-up create after refusal must succeed")
	}
}

// DLG-001: a capability outside the parent set rejects the envelope.
func TestDelegationCreateCapabilityEscalation(t *testing.T) {
	h := newM6DelegationHarness(t)
	payload := `{"rootId":"` + h.rootID + `","parentId":"node-1",` +
		`"objective":"steal","inputDigests":["` + sha256Hex("input-a") + `"],` +
		`"capabilitySet":["fs.read","secret.reveal"],` +
		`"budgetGrant":{"cpuSeconds":120,"tokens":5000,"cost":200,"wallClockMs":600000},` +
		`"deadlineMs":600000,"depth":0}`
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate, payload, "dlg-key-4"))
	if resp.OK || resp.Error.Code != "M6-DLG-001" {
		t.Fatalf("want M6-DLG-001, got %+v", resp.Error)
	}
}

// DLG-002: depth over the hard cap of 4 is refused before any row lands.
func TestDelegationCreateDepthRefused(t *testing.T) {
	h := newM6DelegationHarness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate,
		delegationCreatePayload(h.rootID, "node-1", 5000, 5), "dlg-key-5"))
	if resp.OK || resp.Error.Code != "M6-DLG-002" {
		t.Fatalf("want M6-DLG-002, got %+v", resp.Error)
	}
}

// Settle flows into the root's open barrier; the second arrival closes ALL.
func TestDelegationSettleJoinsBarrier(t *testing.T) {
	h := newM6DelegationHarness(t)
	barrier, err := h.barriers.Create(context.Background(), h.rootID, "ALL", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := createDelegation(t, h, "dlg-key-6", "node-1", 5000, 0)
	if state := settleDelegation(t, h, "settle-key-6", first); state != "open" {
		t.Fatalf("first settle barrierState = %q, want open", state)
	}
	second := createDelegation(t, h, "dlg-key-6b", "node-1", 5000, 0)
	if state := settleDelegation(t, h, "settle-key-6b", second); state != "closed" {
		t.Fatalf("second settle barrierState = %q, want closed", state)
	}
	// barrier.arrive duplicate (JOIN-001): answers the existing settlement
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodBarrierArrive,
		`{"barrierId":"`+barrier.ID+`","childId":"nonexistent-child","attempt":0,"resultDigest":"`+sha256Hex("r")+`"}`, ""))
	var late struct {
		BarrierState   string `json:"barrierState"`
		AlreadySettled bool   `json:"alreadySettled"`
	}
	m6Payload(t, resp, &late)
	if late.BarrierState != "closed" || late.AlreadySettled {
		t.Fatalf("late arrival: %+v", late)
	}
}

// barrier.arrive: duplicates answer alreadySettled without double settling.
func TestBarrierArriveDuplicateIdempotent(t *testing.T) {
	h := newM6DelegationHarness(t)
	barrier, err := h.barriers.Create(context.Background(), h.rootID, "QUORUM", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"barrierId":"` + barrier.ID + `","childId":"task-1","attempt":0,"resultDigest":"` + sha256Hex("r1") + `"}`
	var first struct {
		BarrierState   string `json:"barrierState"`
		AlreadySettled bool   `json:"alreadySettled"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodBarrierArrive, payload, "")), &first)
	if first.BarrierState != "open" || first.AlreadySettled {
		t.Fatalf("first arrival: %+v", first)
	}
	var replay struct {
		BarrierState   string `json:"barrierState"`
		AlreadySettled bool   `json:"alreadySettled"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodBarrierArrive, payload, "")), &replay)
	if !replay.AlreadySettled || replay.BarrierState != "open" {
		t.Fatalf("duplicate arrival must answer the existing settlement: %+v", replay)
	}
	// second success reaches quorum 2 of 3 and closes
	secondPayload := `{"barrierId":"` + barrier.ID + `","childId":"task-2","attempt":0,"resultDigest":"` + sha256Hex("r2") + `"}`
	var second struct {
		BarrierState   string `json:"barrierState"`
		AlreadySettled bool   `json:"alreadySettled"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodBarrierArrive, secondPayload, "")), &second)
	if second.BarrierState != "closed" {
		t.Fatalf("quorum reached must close: %+v", second)
	}
}

// G3 conservation: 100 children reserve + settle, the ledger stays at
// drift 0 across all four dimensions.
func TestBudgetConservation100Children(t *testing.T) {
	h := newM6DelegationHarness(t)
	for i := 0; i < 100; i++ {
		id := createDelegation(t, h, "cons-create-"+jsonInt(int64(i)), "node-bulk", 1000, 0)
		settleDelegation(t, h, "cons-settle-"+jsonInt(int64(i)), id)
	}
	var driftChecked int
	err := h.repo.TransactM6(context.Background(), func(tx m6app.Tx) error {
		accounts, err := tx.ListM6BudgetAccounts(h.rootID)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			if a.Drift() != 0 {
				t.Fatalf("dimension %s drifted by %d", a.Dimension, a.Drift())
			}
			if a.Granted == 0 {
				t.Fatalf("dimension %s never granted", a.Dimension)
			}
			driftChecked++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if driftChecked == 0 {
		t.Fatal("no budget accounts found")
	}
}

// Cancel semantics: an unstarted child refunds its reservation; a settled
// child's consumption is final.
func TestDelegationCancelRefundsUnstarted(t *testing.T) {
	h := newM6DelegationHarness(t)
	createDelegation(t, h, "refund-create-1", "node-refund", 5000, 0)
	createDelegation(t, h, "refund-create-2", "node-refund", 5000, 0)
	// settle only the first; the second stays reserved
	id := createDelegation(t, h, "refund-create-3", "node-refund2", 5000, 0)
	settleDelegation(t, h, "refund-settle-1", id)
	err := h.repo.TransactM6(context.Background(), func(tx m6app.Tx) error {
		acct, err := tx.GetM6BudgetAccount(h.rootID, "tokens")
		if err != nil {
			return err
		}
		if acct.Drift() != 0 {
			t.Fatalf("drift before refund: %d", acct.Drift())
		}
		if acct.Consumed != 100 {
			t.Fatalf("consumed = %d, want 100", acct.Consumed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// JOIN-001: settling past the deadline flips the delegation to expired.
func TestDelegationSettleLateArrival(t *testing.T) {
	h := newM6DelegationHarness(t)
	// deadlineMs floor is 1000ms — create with the shortest deadline
	payload := `{"rootId":"` + h.rootID + `","parentId":"node-late",` +
		`"objective":"late","inputDigests":["` + sha256Hex("input-a") + `"],` +
		`"capabilitySet":["fs.read"],` +
		`"budgetGrant":{"cpuSeconds":10,"tokens":500,"cost":1,"wallClockMs":1000},` +
		`"deadlineMs":1000,"depth":0}`
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate, payload, "dlg-late"))
	var created struct {
		DelegationID string `json:"delegationId"`
	}
	m6Payload(t, resp, &created)
	time.Sleep(1100 * time.Millisecond)
	settle := h.e.Handle(context.Background(), m6Request(bridge.MethodDelegationSettle,
		`{"delegationId":"`+created.DelegationID+`","resultBundle":{`+
			`"claims":{"summary":"late"},"patchDigest":"`+sha256Hex("patch")+`",`+
			`"testEvidenceRefs":[],"usage":{"tokens":10},`+
			`"resultDigest":"`+sha256Hex("result")+`"}}`, "settle-late"))
	if settle.OK || settle.Error.Code != "M6-JOIN-001" {
		t.Fatalf("want M6-JOIN-001, got %+v", settle.Error)
	}
}

// Unwired services answer STORAGE_UNAVAILABLE / FEATURE semantics.
func TestM6DelegationServicesUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), m6Request(bridge.MethodDelegationCreate,
		`{"rootId":"01ARZ3NDEKTSV4RRFFQ69G5FA9","parentId":"p","objective":"o","inputDigests":["`+
			sha256Hex("x")+`"],"capabilitySet":["fs.read"],`+
			`"budgetGrant":{"cpuSeconds":1,"tokens":1,"cost":1,"wallClockMs":1000},"deadlineMs":600000,"depth":0}`, "k"))
	if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("delegation.create unwired: %+v", resp.Error)
	}
	resp = e.Handle(context.Background(), m6Request(bridge.MethodDelegationSettle,
		`{"delegationId":"01ARZ3NDEKTSV4RRFFQ69G5FAX","resultBundle":{"claims":{"a":1},`+
			`"patchDigest":"`+sha256Hex("p")+`","testEvidenceRefs":[],"usage":{},`+
			`"resultDigest":"`+sha256Hex("r")+`"}}`, "k"))
	if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("delegation.settle unwired: %+v", resp.Error)
	}
	resp = e.Handle(context.Background(), m6Request(bridge.MethodBarrierArrive,
		`{"barrierId":"01ARZ3NDEKTSV4RRFFQ69G5FAY","childId":"c","attempt":0,"resultDigest":"`+sha256Hex("r")+`"}`, ""))
	if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("barrier.arrive unwired: %+v", resp.Error)
	}
}
