package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

const reviewDigestA = "06fe2b87000000000000000000000000000000000000000000000000000000aa"
const reviewDigestB = "06fe2b87000000000000000000000000000000000000000000000000000000bb"

// pauseRunForReview drives a running run into paused_review and records the
// pending approval digest, mimicking what a future tool flow (M4-D+) does
// when it pauses a run for review.
func pauseRunForReview(t *testing.T, store *storage.Store, runID string, version int64, digest string) {
	t.Helper()
	err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		now := time.Now().UTC()
		events, err := tx.ListEvents(runID)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"schemaVersion": 1, "runId": runID, "approvalDigest": digest})
		if err := tx.AppendEvent(agentrun.RunEvent{
			ID:        ulid.Make().String(),
			RunID:     runID,
			Sequence:  int64(len(events)) + 1,
			EventType: agentrun.EventReviewRequested,
			Payload:   payload,
			CreatedAt: now,
		}); err != nil {
			return err
		}
		_, err = tx.TransitionRun(runID, version, agentrun.RunPausedReview, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func decideReview(t *testing.T, e *Engine, runID string, version int64, digest, decision, key string) bridge.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"runId":           runID,
		"expectedVersion": version,
		"approvalDigest":  digest,
		"decision":        decision,
	})
	return e.Handle(context.Background(), agentRunRequest(bridge.MethodReviewDecide, string(payload), key))
}

func getRunStatus(t *testing.T, e *Engine, runID string) (agentrun.RunStatus, int64) {
	t.Helper()
	r := e.Handle(context.Background(), validRequest(string(bridge.MethodAgentRunGet), `{"runId":"`+runID+`"}`))
	if !r.OK {
		t.Fatalf("get: %#v", r)
	}
	body, _ := json.Marshal(r.Payload)
	var dto agentRunDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatal(err)
	}
	return dto.Status, dto.Version
}

func TestReviewDecideApproveResumeAndReplay(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	run := startAgentRun(t, e, sessionID, "review-start-1")
	pauseRunForReview(t, store, run.ID, run.Version, reviewDigestA)

	res := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "approved", "review-decide-1")
	if !res.OK {
		t.Fatalf("decide: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
	body, _ := json.Marshal(res.Payload)
	var out struct {
		Run    agentRunDTO `json:"run"`
		Review struct {
			ID             string `json:"id"`
			RunID          string `json:"runId"`
			ApprovalDigest string `json:"approvalDigest"`
			Decision       string `json:"decision"`
			DecidedBy      string `json:"decidedBy"`
		} `json:"review"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Run.Status != agentrun.RunRunning || out.Run.Version != run.Version+2 {
		t.Fatalf("run after approve=%+v", out.Run)
	}
	if out.Review.RunID != run.ID || out.Review.ApprovalDigest != reviewDigestA || out.Review.Decision != "approved" || out.Review.DecidedBy == "" {
		t.Fatalf("review=%+v", out.Review)
	}
	if _, err := ulid.ParseStrict(out.Review.ID); err != nil {
		t.Fatalf("review id %q: %v", out.Review.ID, err)
	}

	// Replay with the same key and payload returns the recorded decision.
	replay := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "approved", "review-decide-1")
	if !replay.OK {
		t.Fatalf("replay: %#v", replay)
	}
	replayBody, _ := json.Marshal(replay.Payload)
	var replayOut struct {
		Run    agentRunDTO `json:"run"`
		Review struct {
			ID string `json:"id"`
		} `json:"review"`
	}
	if err := json.Unmarshal(replayBody, &replayOut); err != nil {
		t.Fatal(err)
	}
	if replayOut.Review.ID != out.Review.ID || replayOut.Run.Version != out.Run.Version {
		t.Fatalf("replay mismatch: %+v vs %+v", replayOut, out)
	}

	// Same key with a different decision conflicts.
	conflict := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "rejected", "review-decide-1")
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict=%#v", conflict)
	}
}

func TestReviewDecideDigestMismatchHasNoSideEffect(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	run := startAgentRun(t, e, sessionID, "review-start-2")
	pauseRunForReview(t, store, run.ID, run.Version, reviewDigestA)

	res := decideReview(t, e, run.ID, run.Version+1, reviewDigestB, "approved", "review-decide-2")
	if res.OK || res.Error.Code != "REVIEW_DIGEST_MISMATCH" {
		t.Fatalf("mismatch=%#v", res)
	}

	// The run is untouched: still paused_review at the same version.
	status, version := getRunStatus(t, e, run.ID)
	if status != agentrun.RunPausedReview || version != run.Version+1 {
		t.Fatalf("run mutated on mismatch: status=%s version=%d", status, version)
	}

	// The correct digest with a fresh key still succeeds afterwards.
	ok := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "approved", "review-decide-3")
	if !ok.OK {
		t.Fatalf("decide after mismatch: %#v", ok)
	}
}

func TestReviewDecideRejectKeepsRunPaused(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	run := startAgentRun(t, e, sessionID, "review-start-3")
	pauseRunForReview(t, store, run.ID, run.Version, reviewDigestA)

	res := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "rejected", "review-decide-4")
	if !res.OK {
		t.Fatalf("reject: %#v", res)
	}
	status, version := getRunStatus(t, e, run.ID)
	if status != agentrun.RunPausedReview || version != run.Version+1 {
		t.Fatalf("reject must keep the run paused: status=%s version=%d", status, version)
	}

	// A later approve on the same pending digest still resumes the run.
	approve := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "approved", "review-decide-5")
	if !approve.OK {
		t.Fatalf("approve after reject: %#v", approve)
	}
	status, _ = getRunStatus(t, e, run.ID)
	if status != agentrun.RunRunning {
		t.Fatalf("status after approve=%s", status)
	}
}

func TestReviewDecideValidatesStateVersionAndInput(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	run := startAgentRun(t, e, sessionID, "review-start-4")

	// A running run is not awaiting review.
	notPaused := decideReview(t, e, run.ID, run.Version, reviewDigestA, "approved", "review-decide-6")
	if notPaused.OK || notPaused.Error.Code != "AGENT_RUN_TRANSITION_INVALID" {
		t.Fatalf("notPaused=%#v", notPaused)
	}

	pauseRunForReview(t, store, run.ID, run.Version, reviewDigestA)

	// Stale expectedVersion is a CAS conflict.
	stale := decideReview(t, e, run.ID, run.Version, reviewDigestA, "approved", "review-decide-7")
	if stale.OK || stale.Error.Code != "RUN_VERSION_CONFLICT" {
		t.Fatalf("stale=%#v", stale)
	}

	// Missing idempotency key.
	payload, _ := json.Marshal(map[string]any{
		"runId":           run.ID,
		"expectedVersion": run.Version + 1,
		"approvalDigest":  reviewDigestA,
		"decision":        "approved",
	})
	noKey := e.Handle(context.Background(), validRequest(string(bridge.MethodReviewDecide), string(payload)))
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("noKey=%#v", noKey)
	}

	// Malformed digest / unknown decision fail schema validation.
	badDigest := decideReview(t, e, run.ID, run.Version+1, "06fe2b87", "approved", "review-decide-8")
	if badDigest.OK || badDigest.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("badDigest=%#v", badDigest)
	}
	badDecision := decideReview(t, e, run.ID, run.Version+1, reviewDigestA, "maybe", "review-decide-9")
	if badDecision.OK || badDecision.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("badDecision=%#v", badDecision)
	}
}

func registerWorkspace(t *testing.T, e *Engine, root, key string) bridge.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"rootPath": root})
	return e.Handle(context.Background(), agentRunRequest(bridge.MethodWorkspaceRegister, string(payload), key))
}

func grantWorkspace(t *testing.T, e *Engine, registrationID string, paths, operations []string, ttl int64, key string) bridge.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"registrationId": registrationID,
		"scope":          map[string]any{"paths": paths, "operations": operations},
		"ttlSeconds":     ttl,
	})
	return e.Handle(context.Background(), agentRunRequest(bridge.MethodWorkspaceGrant, string(payload), key))
}

func leaseWorkspace(t *testing.T, e *Engine, grantID string, ttl int64, key string) bridge.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"grantId": grantID, "ttlSeconds": ttl})
	return e.Handle(context.Background(), agentRunRequest(bridge.MethodWorkspaceLease, string(payload), key))
}

type workspaceRegistrationOut struct {
	ID            string `json:"id"`
	CanonicalRoot string `json:"canonicalRoot"`
	RootDigest    string `json:"rootDigest"`
	Status        string `json:"status"`
	Version       int64  `json:"version"`
}

func decodePayloadInto[T any](t *testing.T, r bridge.Response) T {
	t.Helper()
	if !r.OK {
		t.Fatalf("response: code=%s msg=%s", r.Error.Code, r.Error.Message)
	}
	body, _ := json.Marshal(r.Payload)
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWorkspaceRegisterIdentityIdempotent(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	root := t.TempDir()

	res := registerWorkspace(t, e, root, "ws-register-1")
	reg := decodePayloadInto[workspaceRegistrationOut](t, res)
	if _, err := ulid.ParseStrict(reg.ID); err != nil {
		t.Fatalf("registration id %q: %v", reg.ID, err)
	}
	if reg.Status != "active" || reg.Version != 1 || reg.CanonicalRoot == "" {
		t.Fatalf("registration=%+v", reg)
	}
	sum := sha256.Sum256([]byte(reg.CanonicalRoot))
	if reg.RootDigest != hex.EncodeToString(sum[:]) {
		t.Fatalf("root digest %q does not match sha256(canonicalRoot)", reg.RootDigest)
	}

	// Replay with the same key returns the same registration.
	replay := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, root, "ws-register-1"))
	if replay.ID != reg.ID {
		t.Fatalf("replay created a new registration: %+v vs %+v", replay, reg)
	}

	// A different key on the same canonical root is idempotent by identity.
	again := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, root, "ws-register-2"))
	if again.ID != reg.ID {
		t.Fatalf("re-register created a new registration: %+v vs %+v", again, reg)
	}

	// Nonexistent / relative roots are rejected.
	missing := registerWorkspace(t, e, root+string([]rune{0})+"nope", "ws-register-3")
	if missing.OK || missing.Error.Code != "WORKSPACE_ROOT_INVALID" {
		t.Fatalf("missing=%#v", missing)
	}
	relative := registerWorkspace(t, e, "some/relative/path", "ws-register-4")
	if relative.OK || relative.Error.Code != "WORKSPACE_ROOT_INVALID" {
		t.Fatalf("relative=%#v", relative)
	}
}

func TestWorkspaceGrantValidatesScopeAndRegistration(t *testing.T) {
	e, _, store := agentRunEngine(t)
	reg := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, t.TempDir(), "ws-register-5"))

	type grantOut struct {
		ID             string `json:"id"`
		RegistrationID string `json:"registrationId"`
		Status         string `json:"status"`
		ExpiresAt      time.Time
	}
	grant := decodePayloadInto[struct {
		ID             string `json:"id"`
		RegistrationID string `json:"registrationId"`
		Status         string `json:"status"`
	}](t, grantWorkspace(t, e, reg.ID, []string{"src/**", "docs/readme.md"}, []string{"read", "write"}, 3600, "ws-grant-1"))
	if grant.RegistrationID != reg.ID || grant.Status != "active" {
		t.Fatalf("grant=%+v", grant)
	}

	// Unknown registration.
	unknown := grantWorkspace(t, e, "01ARZ3NDEKTSV4RRFFQ69G5FAV", []string{"src/**"}, []string{"read"}, 3600, "ws-grant-2")
	if unknown.OK || unknown.Error.Code != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown=%#v", unknown)
	}

	// Path escape and unknown operations are rejected.
	escape := grantWorkspace(t, e, reg.ID, []string{"../outside"}, []string{"read"}, 3600, "ws-grant-3")
	if escape.OK || escape.Error.Code != "WORKSPACE_SCOPE_INVALID" {
		t.Fatalf("escape=%#v", escape)
	}
	badOp := grantWorkspace(t, e, reg.ID, []string{"src/**"}, []string{"delete"}, 3600, "ws-grant-4")
	if badOp.OK || badOp.Error.Code != "WORKSPACE_SCOPE_INVALID" {
		t.Fatalf("badOp=%#v", badOp)
	}
	dupOp := grantWorkspace(t, e, reg.ID, []string{"src/**"}, []string{"read", "read"}, 3600, "ws-grant-5")
	if dupOp.OK || dupOp.Error.Code != "WORKSPACE_SCOPE_INVALID" {
		t.Fatalf("dupOp=%#v", dupOp)
	}

	// A revoked registration rejects new grants.
	if err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		current, err := tx.GetRegistration(reg.ID)
		if err != nil {
			return err
		}
		current.Status = agentrun.RegistrationRevoked
		current.Version++
		current.UpdatedAt = time.Now().UTC()
		return tx.PutRegistration(current)
	}); err != nil {
		t.Fatal(err)
	}
	revoked := grantWorkspace(t, e, reg.ID, []string{"src/**"}, []string{"read"}, 3600, "ws-grant-6")
	if revoked.OK || revoked.Error.Code != "WORKSPACE_INACTIVE" {
		t.Fatalf("revoked=%#v", revoked)
	}
	_ = grantOut{}
}

func TestWorkspaceLeaseFencingAndExpiry(t *testing.T) {
	e, _, store := agentRunEngine(t)
	reg := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, t.TempDir(), "ws-register-6"))

	type grantOut struct {
		ID        string    `json:"id"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	grant := decodePayloadInto[grantOut](t, grantWorkspace(t, e, reg.ID, []string{"src/**"}, []string{"read"}, 60, "ws-grant-7"))

	type leaseOut struct {
		ID           string    `json:"id"`
		GrantID      string    `json:"grantId"`
		FencingToken int64     `json:"fencingToken"`
		ExpiresAt    time.Time `json:"expiresAt"`
		Status       string    `json:"status"`
	}
	first := decodePayloadInto[leaseOut](t, leaseWorkspace(t, e, grant.ID, 3600, "ws-lease-1"))
	if first.GrantID != grant.ID || first.FencingToken != 1 || first.Status != "active" {
		t.Fatalf("first lease=%+v", first)
	}
	// The lease never outlives its grant.
	if first.ExpiresAt.After(grant.ExpiresAt) {
		t.Fatalf("lease outlives grant: lease=%s grant=%s", first.ExpiresAt, grant.ExpiresAt)
	}

	// Fencing tokens increase monotonically per grant.
	second := decodePayloadInto[leaseOut](t, leaseWorkspace(t, e, grant.ID, 900, "ws-lease-2"))
	if second.FencingToken != 2 {
		t.Fatalf("fencing token not monotonic: %+v vs %+v", second, first)
	}

	// Replay returns the original lease.
	replay := decodePayloadInto[leaseOut](t, leaseWorkspace(t, e, grant.ID, 3600, "ws-lease-1"))
	if replay.ID != first.ID || replay.FencingToken != first.FencingToken {
		t.Fatalf("replay mismatch: %+v vs %+v", replay, first)
	}

	// Unknown grant.
	unknown := leaseWorkspace(t, e, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 900, "ws-lease-3")
	if unknown.OK || unknown.Error.Code != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown=%#v", unknown)
	}

	// An expired grant cannot be leased.
	var expiredGrantID string
	if err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		now := time.Now().UTC()
		scope, _ := json.Marshal(map[string]any{"operations": []string{"read"}, "paths": []string{"src/**"}})
		g := agentrun.WorkspaceGrant{
			ID:             ulid.Make().String(),
			RegistrationID: reg.ID,
			Scope:          scope,
			ExpiresAt:      now.Add(-time.Minute),
			Status:         agentrun.GrantActive,
			CreatedAt:      now.Add(-2 * time.Minute),
			UpdatedAt:      now.Add(-2 * time.Minute),
		}
		expiredGrantID = g.ID
		return tx.PutGrant(g)
	}); err != nil {
		t.Fatal(err)
	}
	expired := leaseWorkspace(t, e, expiredGrantID, 900, "ws-lease-4")
	if expired.OK || expired.Error.Code != "WORKSPACE_INACTIVE" {
		t.Fatalf("expired=%#v", expired)
	}

	// Missing idempotency key.
	noKey := e.Handle(context.Background(), validRequest(string(bridge.MethodWorkspaceLease), `{"grantId":"`+grant.ID+`","ttlSeconds":900}`))
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("noKey=%#v", noKey)
	}
}
