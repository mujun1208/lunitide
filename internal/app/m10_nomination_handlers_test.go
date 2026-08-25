// M10 nomination bridge tests: the full journey over the engine handle
// path — nominate -> nomination.list -> confirmCandidate settles decided ->
// withdraw guards. Also verifies the M10-ME-002 error family.
package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newNominationEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m10nom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	mem := m8app.NewMemoryService(repo, "local-user")
	e.SetM8MemoryServices(mem)
	e.SetM10NominationService(m8app.NewNominationService(repo, mem))
	return e
}

func nominationRequest(method, payload string) bridge.Request {
	return bridge.Request{Version: bridge.Version, Kind: "request", ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Method: method, SentAt: time.Now().UTC(), Payload: json.RawMessage(payload), DeadlineMS: 3000}
}

const nominatePayload = `{"subjectId":"user-1","payload":{"content":"prefer Go examples","scopeId":"scope-1","sensitivity":"private","leaves":[{"jsonPointer":"/content","evidenceRef":"artifact://run-1/evidence-a","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},"reason":"asked for Go three times","sourceSessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`

func TestNominationLifecycleThroughBridge(t *testing.T) {
	e := newNominationEngine(t)
	ctx := context.Background()

	created := e.Handle(ctx, nominationRequest("memory.nominate", nominatePayload))
	if !created.OK {
		t.Fatalf("nominate failed: %+v", created.Error)
	}
	var createdPayload struct {
		NominationID      string `json:"nominationId"`
		CandidateID       string `json:"candidateId"`
		ConfirmationToken string `json:"confirmationToken"`
		State             string `json:"state"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	if len(createdPayload.NominationID) != 26 || len(createdPayload.CandidateID) != 26 ||
		len(createdPayload.ConfirmationToken) != 64 || createdPayload.State != "nominated" {
		t.Fatalf("nominate result invalid: %+v", createdPayload)
	}

	listed := e.Handle(ctx, nominationRequest("memory.nomination.list", `{"state":"nominated","limit":50}`))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), "asked for Go three times") {
		t.Fatalf("nomination.list missing entry: %+v", listed.Payload)
	}

	// 0061 confirm path settles the nomination as decided.
	confirm := e.Handle(ctx, nominationRequest("memory.confirmCandidate", `{"candidateId":"`+createdPayload.CandidateID+`","confirmationToken":"`+createdPayload.ConfirmationToken+`","action":"confirm","requestId":"req-1"}`))
	if !confirm.OK {
		t.Fatalf("confirm failed: %+v", confirm.Error)
	}
	decided := e.Handle(ctx, nominationRequest("memory.nomination.list", `{"state":"decided","limit":50}`))
	if !decided.OK || !strings.Contains(string(mustJSON(decided.Payload)), createdPayload.NominationID) {
		t.Fatalf("nomination not decided after confirm: %+v", decided.Payload)
	}

	// Terminal rows answer M10-ME-002 on withdraw.
	withdraw := e.Handle(ctx, nominationRequest("memory.nomination.withdraw", `{"nominationId":"`+createdPayload.NominationID+`"}`))
	if withdraw.OK || withdraw.Error.Code != "M10-ME-002" {
		t.Fatalf("terminal withdraw = %+v, want M10-ME-002", withdraw.Error)
	}
}

func TestNominationWithdrawFlow(t *testing.T) {
	e := newNominationEngine(t)
	ctx := context.Background()
	created := e.Handle(ctx, nominationRequest("memory.nominate", nominatePayload))
	if !created.OK {
		t.Fatalf("nominate failed: %+v", created.Error)
	}
	var createdPayload struct {
		NominationID string `json:"nominationId"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	withdrawn := e.Handle(ctx, nominationRequest("memory.nomination.withdraw", `{"nominationId":"`+createdPayload.NominationID+`","actor":"local-user"}`))
	if !withdrawn.OK {
		t.Fatalf("withdraw failed: %+v", withdrawn.Error)
	}
	listed := e.Handle(ctx, nominationRequest("memory.nomination.list", `{"state":"withdrawn","limit":50}`))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), createdPayload.NominationID) {
		t.Fatalf("withdrawn list missing entry: %+v", listed.Payload)
	}
}

func TestNominationBridgeValidation(t *testing.T) {
	e := newNominationEngine(t)
	ctx := context.Background()
	cases := []struct {
		method, payload string
	}{
		{"memory.nominate", `{"subjectId":"user-1","payload":{"content":"x","scopeId":"scope-1","sensitivity":"private","leaves":[]},"reason":"r"`},
		{"memory.nominate", `{"subjectId":"user-1","payload":{"content":"x","scopeId":"scope-1","sensitivity":"private","leaves":[{"jsonPointer":"/content","evidenceRef":"artifact://r/e","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},"reason":""}`},
		{"memory.nomination.list", `{"state":"archived"}`},
		{"memory.nomination.list", `{"limit":101}`},
		{"memory.nomination.withdraw", `{"nominationId":"not-a-ulid"}`},
	}
	for _, tc := range cases {
		resp := e.Handle(ctx, nominationRequest(tc.method, tc.payload))
		if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%s %s = %+v, want BRIDGE_SCHEMA_INVALID", tc.method, tc.payload, resp.Error)
		}
	}
	// Unknown nomination answers M10-ME-001.
	resp := e.Handle(ctx, nominationRequest("memory.nomination.withdraw", `{"nominationId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
	if resp.OK || resp.Error.Code != "M10-ME-001" {
		t.Fatalf("unknown withdraw = %+v, want M10-ME-001", resp.Error)
	}
}

func TestNominationServiceUnavailable(t *testing.T) {
	e := NewEngine(nil, "test")
	ctx := context.Background()
	cases := map[string]string{
		"memory.nominate":              `{"subjectId":"user-1","payload":{"content":"x","scopeId":"scope-1","sensitivity":"private","leaves":[{"jsonPointer":"/content","evidenceRef":"artifact://r/e","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},"reason":"r"}`,
		"memory.nomination.list":       `{}`,
		"memory.nomination.withdraw":   `{"nominationId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
	}
	for method, payload := range cases {
		resp := e.Handle(ctx, nominationRequest(method, payload))
		if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" {
			t.Fatalf("%s unwired = %+v, want STORAGE_UNAVAILABLE", method, resp.Error)
		}
	}
}
